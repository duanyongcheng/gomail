package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/oauth"
	"golang.org/x/oauth2"
)

const (
	googleOAuthPurpose = "google_oauth_settings"
	googleOAuthRecord  = "default"
)

type storedGoogleOAuth struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// GoogleOAuthConfig is the safe status and settings projection exposed to the UI.
// Plaintext client secrets are never returned to the frontend.
type GoogleOAuthConfig struct {
	Configured   bool   `json:"configured"`
	Source       string `json:"source"`
	ClientID     string `json:"clientId"`
	HasSecret    bool   `json:"hasSecret"`
	HasAppConfig bool   `json:"hasAppConfig"`
	AppClientID  string `json:"appClientId"`
	AppHasSecret bool   `json:"appHasSecret"`
	KeyringError bool   `json:"keyringError"`
}

type SaveGoogleOAuthConfigRequest struct {
	ClientID           string `json:"clientId"`
	ClientSecret       string `json:"clientSecret"`
	KeepExistingSecret bool   `json:"keepExistingSecret"`
}

func hasDisallowedOAuthChars(value string) bool {
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validateGoogleOAuthInput(clientID, clientSecret string) (string, string, error) {
	id := strings.TrimSpace(clientID)
	secret := strings.TrimSpace(clientSecret)
	if id == "" {
		return "", "", errors.New("Google OAuth Client ID is required")
	}
	if len(id) > 512 {
		return "", "", errors.New("Google OAuth Client ID is too long")
	}
	if len(secret) > 2048 {
		return "", "", errors.New("Google OAuth Client Secret is too long")
	}
	if hasDisallowedOAuthChars(id) {
		return "", "", errors.New("Google OAuth Client ID contains whitespace or control characters")
	}
	if hasDisallowedOAuthChars(secret) {
		return "", "", errors.New("Google OAuth Client Secret contains whitespace or control characters")
	}
	return id, secret, nil
}

func (r *Runtime) googleOAuthConfigLocked() *oauth2.Config {
	if r != nil && r.googleOAuth != nil && r.googleOAuth.ClientID != "" {
		return oauth.GoogleWithCredentials(r.googleOAuth.ClientID, r.googleOAuth.ClientSecret)
	}
	if r != nil && r.googleOAuthLocked {
		return oauth.GoogleWithCredentials("", "")
	}
	return oauth.Google()
}

func (r *Runtime) googleOAuthConfig() *oauth2.Config {
	if r == nil {
		return oauth.Google()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.googleOAuthConfigLocked()
}

func (r *Runtime) googleOAuthStatus() (*oauth2.Config, bool) {
	if r == nil {
		return oauth.Google(), false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.googleOAuthConfigLocked(), r.googleOAuthLocked
}

func (r *Runtime) googleOAuthProjectionLocked() GoogleOAuthConfig {
	if r != nil && r.googleOAuthLocked {
		return GoogleOAuthConfig{
			Configured:   false,
			Source:       "none",
			HasAppConfig: true,
			KeyringError: true,
		}
	}
	if r != nil && r.googleOAuth != nil && r.googleOAuth.ClientID != "" {
		return GoogleOAuthConfig{
			Configured:   true,
			Source:       "app",
			ClientID:     r.googleOAuth.ClientID,
			HasSecret:    r.googleOAuth.ClientSecret != "",
			HasAppConfig: true,
			AppClientID:  r.googleOAuth.ClientID,
			AppHasSecret: r.googleOAuth.ClientSecret != "",
		}
	}
	fallback := oauth.Google()
	return GoogleOAuthConfig{
		Configured: fallback.ClientID != "",
		Source:     oauth.GoogleFallbackSource(),
		ClientID:   fallback.ClientID,
		HasSecret:  fallback.ClientSecret != "",
	}
}

func (r *Runtime) decryptGoogleOAuthLocked(raw []byte) error {
	if len(raw) == 0 {
		r.googleOAuth = nil
		r.googleOAuthLocked = false
		return nil
	}
	if r.crypto == nil {
		r.googleOAuth = nil
		r.googleOAuthLocked = true
		return &domain.AccessError{State: domain.ConnectionCredentialsUnavailable}
	}
	plain, err := r.crypto.Decrypt(googleOAuthPurpose, googleOAuthRecord, raw)
	if err != nil {
		r.googleOAuth = nil
		r.googleOAuthLocked = true
		return &domain.AccessError{State: domain.ConnectionCredentialsUnavailable}
	}
	var cfg storedGoogleOAuth
	if json.Unmarshal(plain, &cfg) != nil || strings.TrimSpace(cfg.ClientID) == "" {
		r.googleOAuth = nil
		r.googleOAuthLocked = true
		return &domain.AccessError{State: domain.ConnectionCredentialsUnavailable}
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	r.googleOAuth = &cfg
	r.googleOAuthLocked = false
	return nil
}

func (r *Runtime) ensureGoogleOAuthLoaded(ctx context.Context) error {
	if r == nil || r.DB == nil {
		return nil
	}
	raw, err := r.DB.GoogleOAuthSettings(ctx)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		r.mu.Lock()
		r.googleOAuth = nil
		r.googleOAuthLocked = false
		r.mu.Unlock()
		return nil
	}
	r.mu.RLock()
	ready := r.googleOAuth != nil && !r.googleOAuthLocked
	r.mu.RUnlock()
	if ready {
		return nil
	}
	cipher, err := r.composeCrypto(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil || cipher == nil {
		r.googleOAuth = nil
		r.googleOAuthLocked = true
		return nil
	}
	_ = r.decryptGoogleOAuthLocked(raw)
	return nil
}

func (r *Runtime) reloadGmailAccountsLocked(ctx context.Context, accounts []domain.Account) {
	cfg := r.googleOAuthConfigLocked()
	for _, a := range accounts {
		if a.Provider != "gmail" {
			continue
		}
		c := &accountConnection{state: domain.ConnectionAvailable}
		r.connections[a.ID] = c
		delete(r.providers, a.ID)
		switch {
		case r.googleOAuthLocked:
			c.state = domain.ConnectionCredentialsUnavailable
		case cfg.ClientID == "":
			c.state = domain.ConnectionConfigurationRequired
		case r.crypto == nil:
			c.state = domain.ConnectionCredentialsUnavailable
		default:
			if err := r.installGmailLocked(a.ID, cfg, c); err != nil {
				c.state = domain.ConnectionCredentialsUnavailable
				var access *domain.AccessError
				if errors.As(err, &access) {
					c.state = access.State
				}
			} else if state, err := r.DB.AccountAuthState(ctx, a.ID); err != nil {
				c.state = domain.ConnectionCredentialsUnavailable
			} else {
				c.state = state
			}
		}
	}
}

func (s *AccountService) GoogleOAuthConfig(ctx context.Context, _ Empty) (GoogleOAuthConfig, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return r.googleOAuthProjectionLocked(), nil
	}
	if err := r.ensureGoogleOAuthLoaded(ctx); err != nil {
		return GoogleOAuthConfig{}, errors.New("Cannot read Google OAuth configuration.")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.googleOAuthProjectionLocked(), nil
}

func (s *AccountService) SaveGoogleOAuthConfig(ctx context.Context, request SaveGoogleOAuthConfigRequest) (GoogleOAuthConfig, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return GoogleOAuthConfig{}, errors.New("Profile database unavailable")
	}
	if !r.authFlowMu.TryLock() {
		return GoogleOAuthConfig{}, errors.New("Finish or cancel sign-in before changing Google OAuth settings")
	}
	defer r.authFlowMu.Unlock()

	id, secret, err := validateGoogleOAuthInput(request.ClientID, request.ClientSecret)
	if err != nil {
		return GoogleOAuthConfig{}, err
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return GoogleOAuthConfig{}, err
	}
	if request.KeepExistingSecret && secret == "" {
		if err := r.ensureGoogleOAuthLoaded(ctx); err != nil {
			return GoogleOAuthConfig{}, errors.New("Cannot read existing Google OAuth configuration")
		}
		r.mu.RLock()
		locked := r.googleOAuthLocked
		if r.googleOAuth != nil {
			secret = r.googleOAuth.ClientSecret
		}
		r.mu.RUnlock()
		if locked {
			return GoogleOAuthConfig{}, errors.New("Saved Google OAuth configuration could not be decrypted; enter a new Client Secret or clear saved configuration")
		}
	}
	stored := storedGoogleOAuth{ClientID: id, ClientSecret: secret}
	plain, err := json.Marshal(stored)
	if err != nil {
		return GoogleOAuthConfig{}, err
	}
	encrypted, err := cipher.Encrypt(googleOAuthPurpose, googleOAuthRecord, plain)
	if err != nil {
		return GoogleOAuthConfig{}, err
	}
	accounts, err := r.DB.Accounts(ctx)
	if err != nil {
		return GoogleOAuthConfig{}, err
	}
	var gmailAccounts []domain.Account
	for _, a := range accounts {
		if a.Provider == "gmail" {
			gmailAccounts = append(gmailAccounts, a)
		}
	}

	r.Scheduler.control.Lock()
	for _, a := range gmailAccounts {
		r.Scheduler.stop(a.ID)
	}
	r.labelManagementMu.Lock()
	r.cloudDraftMu.Lock()
	r.pendingMu.Lock()
	r.outboxMu.Lock()
	var saveErr error
	var out GoogleOAuthConfig
	func() {
		defer func() {
			r.outboxMu.Unlock()
			r.pendingMu.Unlock()
			r.cloudDraftMu.Unlock()
			r.labelManagementMu.Unlock()
			r.Scheduler.control.Unlock()
		}()
		if err := r.DB.SaveGoogleOAuthSettings(ctx, encrypted); err != nil {
			saveErr = err
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.googleOAuth = &stored
		r.googleOAuthLocked = false
		r.reloadGmailAccountsLocked(ctx, gmailAccounts)
		out = r.googleOAuthProjectionLocked()
	}()
	if saveErr != nil {
		for _, a := range gmailAccounts {
			_ = r.Scheduler.Start(a.ID, 0)
		}
		return GoogleOAuthConfig{}, saveErr
	}
	for _, a := range gmailAccounts {
		r.connectionChanged(a.ID)
		_ = r.Scheduler.Start(a.ID, 0)
	}
	return out, nil
}

func (s *AccountService) ClearGoogleOAuthConfig(ctx context.Context, _ Empty) (GoogleOAuthConfig, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return GoogleOAuthConfig{}, errors.New("Profile database unavailable")
	}
	if !r.authFlowMu.TryLock() {
		return GoogleOAuthConfig{}, errors.New("Finish or cancel sign-in before changing Google OAuth settings")
	}
	defer r.authFlowMu.Unlock()

	accounts, err := r.DB.Accounts(ctx)
	if err != nil {
		return GoogleOAuthConfig{}, err
	}
	var gmailAccounts []domain.Account
	for _, a := range accounts {
		if a.Provider == "gmail" {
			gmailAccounts = append(gmailAccounts, a)
		}
	}
	if len(gmailAccounts) > 0 {
		// Best-effort keyring load so falling back to an env/build client can restore accounts.
		_, _ = r.composeCrypto(ctx)
	}

	r.Scheduler.control.Lock()
	for _, a := range gmailAccounts {
		r.Scheduler.stop(a.ID)
	}
	r.labelManagementMu.Lock()
	r.cloudDraftMu.Lock()
	r.pendingMu.Lock()
	r.outboxMu.Lock()
	var clearErr error
	var out GoogleOAuthConfig
	func() {
		defer func() {
			r.outboxMu.Unlock()
			r.pendingMu.Unlock()
			r.cloudDraftMu.Unlock()
			r.labelManagementMu.Unlock()
			r.Scheduler.control.Unlock()
		}()
		if err := r.DB.DeleteGoogleOAuthSettings(ctx); err != nil {
			clearErr = err
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.googleOAuth = nil
		r.googleOAuthLocked = false
		r.reloadGmailAccountsLocked(ctx, gmailAccounts)
		out = r.googleOAuthProjectionLocked()
	}()
	if clearErr != nil {
		for _, a := range gmailAccounts {
			_ = r.Scheduler.Start(a.ID, 0)
		}
		return GoogleOAuthConfig{}, clearErr
	}
	for _, a := range gmailAccounts {
		r.connectionChanged(a.ID)
		_ = r.Scheduler.Start(a.ID, 0)
	}
	return out, nil
}
