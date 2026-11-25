package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/crypto"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/googleapi"
	"github.com/duanyongcheng/gomail/internal/mailnet"
	"github.com/duanyongcheng/gomail/internal/oauth"
	"github.com/duanyongcheng/gomail/internal/provider/gmail"
	"golang.org/x/oauth2"
)

type accountConnection struct {
	state          domain.ConnectionState
	persistPending bool
	cancel         context.CancelFunc
}

type AccountConnection struct {
	AccountID    string                 `json:"accountId"`
	State        domain.ConnectionState `json:"state"`
	CanReconnect bool                   `json:"canReconnect"`
}

func (r *Runtime) restoreAccounts() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	accounts, err := r.DB.Accounts(r.Context)
	if err != nil {
		return err
	}
	has, err := r.DB.HasAuthData(r.Context)
	if err != nil {
		return err
	}
	oauthRaw, err := r.DB.GoogleOAuthSettings(r.Context)
	if err != nil {
		return err
	}
	aiRaw, err := r.DB.AISettings(r.Context)
	if err != nil {
		return err
	}
	var keyErr error
	if has || len(oauthRaw) > 0 || len(aiRaw) > 0 {
		if r.Keyring == nil {
			keyErr = errors.New("Account keyring unavailable")
		} else {
			r.crypto, keyErr = crypto.Load(r.Keyring, true)
		}
	}
	if data, err := r.DB.NetworkSettings(r.Context); err != nil {
		return err
	} else if len(data) > 0 {
		var settings NetworkSettings
		if json.Unmarshal(data, &settings) != nil {
			return errors.New("Invalid network settings")
		}
		r.globalProxy = settings.Proxy
	}
	var failures []error
	if len(oauthRaw) > 0 {
		if keyErr != nil {
			r.googleOAuth = nil
			r.googleOAuthLocked = true
			failures = append(failures, &domain.AccessError{State: domain.ConnectionCredentialsUnavailable})
		} else if err := r.decryptGoogleOAuthLocked(oauthRaw); err != nil {
			failures = append(failures, err)
		}
	} else {
		r.googleOAuth = nil
		r.googleOAuthLocked = false
	}
	if len(aiRaw) > 0 {
		if keyErr != nil {
			r.aiConfig = nil
			r.aiConfigLocked = true
		} else {
			_ = r.decryptAIConfigLocked(aiRaw)
		}
	} else {
		r.aiConfig = nil
		r.aiConfigLocked = false
	}
	cfg := r.googleOAuthConfigLocked()
	for _, a := range accounts {
		if a.Provider != "gmail" {
			c := &accountConnection{state: domain.ConnectionAvailable}
			r.connections[a.ID] = c
			if keyErr != nil || r.crypto == nil {
				c.state = domain.ConnectionCredentialsUnavailable
			} else if config, encrypted, err := r.loadProtocol(r.Context, a.ID); err != nil {
				c.state = domain.ConnectionCredentialsUnavailable
			} else if p, err := r.makeProtocolLocked(a, config, encrypted, c); err != nil {
				c.state = domain.ConnectionConfigurationRequired
			} else {
				r.providers[a.ID] = p
				if state, err := r.DB.AccountAuthState(r.Context, a.ID); err != nil {
					c.state = domain.ConnectionCredentialsUnavailable
				} else {
					c.state = state
				}
			}
			if c.state.Blocked() {
				failures = append(failures, &domain.AccessError{State: c.state})
			}
			continue
		}
		c := &accountConnection{state: domain.ConnectionAvailable}
		r.connections[a.ID] = c
		switch {
		case keyErr != nil || r.googleOAuthLocked:
			c.state = domain.ConnectionCredentialsUnavailable
		case cfg.ClientID == "":
			c.state = domain.ConnectionConfigurationRequired
		case r.crypto == nil:
			c.state = domain.ConnectionReauthRequired
		default:
			if err := r.installGmailLocked(a.ID, cfg, c); err != nil {
				c.state = domain.ConnectionCredentialsUnavailable
				var access *domain.AccessError
				if errors.As(err, &access) {
					c.state = access.State
				}
			} else if state, err := r.DB.AccountAuthState(r.Context, a.ID); err != nil {
				c.state = domain.ConnectionCredentialsUnavailable
			} else {
				c.state = state
			}
		}
		if c.state.Blocked() {
			failures = append(failures, &domain.AccessError{State: c.state})
		}
	}
	return errors.Join(failures...)
}

// Caller holds Runtime.mu. Each installation has a distinct identity, so an
// old request cannot update the state of replacement credentials.
func (r *Runtime) installGmailLocked(account string, cfg *oauth2.Config, c *accountConnection) error {
	vault := &oauth.DatabaseVault{DB: r.DB, Crypto: r.crypto, Context: r.Context, AccountID: account, ClientID: cfg.ClientID}
	if _, err := vault.Load(); err != nil {
		return err
	}
	client, err := r.accountNetworkClientLocked(r.Context, account)
	if err != nil {
		return err
	}
	api := &googleapi.API{Base: r.GmailAPIBase, HTTP: client, Tokens: oauth.NewTokens(cfg, client, vault)}
	api.BeforeRequest = func() error {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if r.connections[account] != c {
			return &domain.AccessError{State: domain.ConnectionReauthRequired}
		}
		if c.state.Blocked() {
			return &domain.AccessError{State: c.state}
		}
		return nil
	}
	api.OnResult = func(err error) { r.observeConnection(account, c, err) }
	r.providers[account] = gmail.New(r.DB, api, account)
	return nil
}

func (r *Runtime) observeConnection(account string, c *accountConnection, err error) {
	state := googleapi.ConnectionFailure(err)
	if state == "" {
		return
	}
	r.mu.Lock()
	if r.connections[account] != c || c.state.Blocked() || c.state == state {
		r.mu.Unlock()
		return
	}
	c.state = state
	if state.Blocked() {
		// A storage failure still blocks in memory. No request is allowed merely
		// because persisting the diagnostic failed.
		c.persistPending = true
		r.persistConnectionLocked(account, c)
	}
	r.mu.Unlock()
	r.connectionChanged(account)
}

func (r *Runtime) persistConnectionLocked(account string, c *accountConnection) {
	if !c.persistPending || c.state == domain.ConnectionReconnecting {
		return
	}
	if err := r.DB.SetAccountAuthState(r.Context, account, c.state); err != nil {
		slog.Warn("account access state persistence deferred")
		return
	}
	c.persistPending = false
}

func (r *Runtime) connectionChanged(account string) {
	if r.OnAccountChanged != nil {
		r.OnAccountChanged(AccountRequest{AccountID: account})
	}
	r.Scheduler.accessChanged(account)
	r.notifyPendingOps()
	r.notifyOutbox()
}
func (r *Runtime) accountBlocked(account string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := r.connections[account]
	return c != nil && c.state.Blocked()
}
func (r *Runtime) blockedAccounts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for id, c := range r.connections {
		r.persistConnectionLocked(id, c)
		if c.state.Blocked() {
			out = append(out, id)
		}
	}
	return out
}

func (s *AccountService) Connections(ctx context.Context, _ Empty) ([]AccountConnection, error) {
	r := s.Runtime
	accounts, err := r.DB.Accounts(ctx)
	if err != nil {
		return nil, errors.New("Cannot read account connection states.")
	}
	out := []AccountConnection{}
	r.mu.RLock()
	defer r.mu.RUnlock()
	configured := r.googleOAuthConfigLocked().ClientID != ""
	for _, account := range accounts {
		state := domain.ConnectionReauthRequired
		if c := r.connections[account.ID]; c != nil {
			state = c.state
		} else if r.providers[account.ID] != nil {
			state = domain.ConnectionAvailable
		}
		out = append(out, AccountConnection{AccountID: account.ID, State: state, CanReconnect: account.Provider == "gmail" && configured})
	}
	return out, nil
}

// Lock ordering matches all account credential replacement/deletion paths. No
// Runtime lock is held while waiting for a provider or a cancelled scheduler.
func (r *Runtime) quiesceAccount(account string) func() {
	r.Scheduler.control.Lock()
	r.Scheduler.stop(account)
	r.labelManagementMu.Lock()
	r.cloudDraftMu.Lock()
	r.pendingMu.Lock()
	r.outboxMu.Lock()
	return func() {
		r.outboxMu.Unlock()
		r.pendingMu.Unlock()
		r.cloudDraftMu.Unlock()
		r.labelManagementMu.Unlock()
		r.Scheduler.control.Unlock()
	}
}

func (s *AccountService) CancelReconnect(_ context.Context, request AccountRequest) (Empty, error) {
	r := s.Runtime
	r.mu.RLock()
	c := r.connections[request.AccountID]
	var cancel context.CancelFunc
	if c != nil {
		cancel = c.cancel
	}
	if r.reconnectAccount == request.AccountID {
		cancel = r.reconnectCancel
	}
	r.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	return Empty{}, nil
}

func (s *AccountService) Reconnect(ctx context.Context, request AccountRequest) (Empty, error) {
	r := s.Runtime
	if !r.authFlowMu.TryLock() {
		return Empty{}, errors.New("Another Google sign-in is already in progress.")
	}
	defer r.authFlowMu.Unlock()
	cfg := r.googleOAuthConfig()
	if cfg.ClientID == "" {
		return Empty{}, &domain.AccessError{State: domain.ConnectionConfigurationRequired}
	}
	if err := ctx.Err(); err != nil {
		return Empty{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context, cancel)
	defer stop()
	r.mu.Lock()
	r.reconnectAccount, r.reconnectCancel = request.AccountID, cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.reconnectAccount, r.reconnectCancel = "", nil
		r.mu.Unlock()
	}()
	account, err := r.DB.Account(ctx, request.AccountID)
	if err != nil || account.Provider != "gmail" {
		return Empty{}, errors.New("Existing Gmail account required.")
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return Empty{}, &domain.AccessError{State: domain.ConnectionCredentialsUnavailable}
	}
	release := r.quiesceAccount(request.AccountID)
	account, err = r.DB.Account(ctx, request.AccountID)
	if err != nil || account.Provider != "gmail" {
		release()
		return Empty{}, errors.New("Existing Gmail account required.")
	}
	old, err := r.DB.AuthData(ctx, account.ID)
	if err != nil {
		release()
		return Empty{}, errors.New("Cannot read existing credentials; no changes were made.")
	}
	r.mu.Lock()
	c := r.connections[account.ID]
	if c == nil {
		c = &accountConnection{state: domain.ConnectionAvailable}
		r.connections[account.ID] = c
	}
	previous := c.state
	c.state, c.cancel = domain.ConnectionReconnecting, cancel
	r.mu.Unlock()
	release()
	r.connectionChanged(account.ID)
	committed := false
	defer func() {
		if committed {
			return
		}
		r.mu.Lock()
		current := r.connections[account.ID] == c
		if current {
			c.state, c.cancel = previous, nil
		}
		r.mu.Unlock()
		if current {
			r.connectionChanged(account.ID)
			_ = r.Scheduler.Start(account.ID, 0)
		}
	}()

	client, err := r.accountNetworkClient(ctx, account.ID)
	if err != nil {
		return Empty{}, err
	}
	token, err := r.Authorize(ctx, cfg, client, r.OpenBrowser)
	if err != nil {
		if ctx.Err() != nil {
			return Empty{}, errors.New("Google reconnection cancelled; saved credentials are unchanged.")
		}
		return Empty{}, err
	}
	if err := oauth.ValidateGmailGrant(token); err != nil {
		return Empty{}, err
	}
	api := &googleapi.API{Base: r.GmailAPIBase, HTTP: client, Tokens: staticToken{token.AccessToken}}
	profile, err := api.Profile(ctx)
	if err != nil {
		return Empty{}, errors.New("Cannot verify the Gmail mailbox; saved credentials are unchanged.")
	}
	if !strings.EqualFold(strings.TrimSpace(profile.Email), strings.TrimSpace(account.Email)) {
		return Empty{}, errors.New("The selected Google mailbox does not match this account; saved credentials are unchanged.")
	}
	encrypted, err := oauth.EncodeToken(cipher, account.ID, cfg.ClientID, token)
	if err != nil {
		return Empty{}, errors.New("Cannot protect replacement credentials.")
	}
	release = r.quiesceAccount(account.ID)
	defer func() { release() }()
	r.mu.Lock()
	if r.connections[account.ID] != c || ctx.Err() != nil {
		r.mu.Unlock()
		return Empty{}, errors.New("The account changed or reconnection was cancelled; no credentials were replaced.")
	}
	current, err := r.DB.Account(ctx, account.ID)
	if err != nil || current.Email != account.Email {
		r.mu.Unlock()
		return Empty{}, errors.New("The account changed during reconnection; no credentials were replaced.")
	}
	ok, err := r.DB.ReplaceAccountCredentials(ctx, account.ID, old, encrypted)
	if err != nil || !ok {
		r.mu.Unlock()
		return Empty{}, errors.New("Credentials changed during reconnection or could not be saved; retry reconnecting.")
	}
	next := &accountConnection{state: domain.ConnectionAvailable}
	r.connections[account.ID] = next
	err = r.installGmailLocked(account.ID, cfg, next)
	if err != nil {
		next.state = domain.ConnectionCredentialsUnavailable
	}
	r.mu.Unlock()
	committed = true
	// Release before starting the scheduler; Start owns scheduler.control.
	release()
	release = func() {}
	r.connectionChanged(account.ID)
	_ = r.Scheduler.Start(account.ID, 0)
	return Empty{}, err
}

func (s *AccountService) login(ctx context.Context, request LoginRequest) (domain.Account, error) {
	r := s.Runtime
	if request.Provider != "gmail" && request.Provider != "google" {
		return domain.Account{}, errors.New("NotImplemented: complete_oauth_flow for this provider (M2)")
	}
	if !r.authFlowMu.TryLock() {
		return domain.Account{}, errors.New("Another Google sign-in is already in progress.")
	}
	defer r.authFlowMu.Unlock()
	cfg := r.googleOAuthConfig()
	if cfg.ClientID == "" {
		return domain.Account{}, &domain.AccessError{State: domain.ConnectionConfigurationRequired}
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context, cancel)
	defer stop()
	client := r.networkClient()
	loginRoute := mailnet.ProxySetting{Mode: "inherit"}
	if request.Proxy != nil {
		loginRoute = *request.Proxy
		if loginRoute.Mode == "" {
			loginRoute.Mode = "inherit"
		}
		r.mu.RLock()
		route, routeErr := mailnet.ResolveProxy(r.globalProxy, loginRoute)
		r.mu.RUnlock()
		if routeErr != nil {
			return domain.Account{}, routeErr
		}
		client = &http.Client{Transport: (mailnet.Dialer{Proxy: route}).HTTPTransport(), Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	routedAccount := ""
	// Repeated sign-in for a known mailbox pins its account-specific route.
	if request.Email != "" {
		digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(request.Email))))
		knownID := "gmail:" + hex.EncodeToString(digest[:16])
		if known, lookupErr := r.DB.Account(ctx, knownID); lookupErr == nil && known.Provider == "gmail" {
			if request.Proxy != nil {
				stored, routeErr := r.DB.AccountNetwork(ctx, knownID)
				if routeErr != nil || !reflect.DeepEqual(stored, loginRoute) {
					return domain.Account{}, errors.New("Use account network settings before reconnecting with a different route")
				}
			}
			client, err = r.accountNetworkClient(ctx, knownID)
			if err != nil {
				return domain.Account{}, err
			}
			routedAccount = knownID
		}
	}
	token, err := r.Authorize(ctx, cfg, client, r.OpenBrowser)
	if err != nil {
		return domain.Account{}, err
	}
	if err := oauth.ValidateGmailGrant(token); err != nil {
		return domain.Account{}, err
	}
	api := &googleapi.API{Base: r.GmailAPIBase, HTTP: client, Tokens: staticToken{token.AccessToken}}
	profile, err := api.Profile(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	digest := sha256.Sum256([]byte(strings.ToLower(profile.Email)))
	id := "gmail:" + hex.EncodeToString(digest[:16])
	if routedAccount != "" && routedAccount != id {
		return domain.Account{}, errors.New("Selected mailbox does not match the account network settings; reconnect the correct account")
	}
	if routedAccount == "" {
		if existing, lookupErr := r.DB.Account(ctx, id); lookupErr == nil && existing.Provider == "gmail" {
			setting, settingErr := r.DB.AccountNetwork(ctx, id)
			if settingErr != nil {
				return domain.Account{}, settingErr
			}
			if !reflect.DeepEqual(setting, loginRoute) {
				return domain.Account{}, errors.New("Use account reconnect to preserve this mailbox's network route")
			}
		}
	}
	encrypted, err := oauth.EncodeToken(cipher, id, cfg.ClientID, token)
	if err != nil {
		return domain.Account{}, err
	}
	release := r.quiesceAccount(id)
	r.mu.Lock()
	account, err := r.DB.Account(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		now, color := time.Now().Unix(), "#0ea5e9"
		account = domain.Account{ID: id, Email: profile.Email, DisplayName: profile.Email, Provider: "gmail", Color: &color, AccountLabel: request.AccountLabel, CreatedAt: now, UpdatedAt: now}
		err = r.DB.CreateGmailAccount(ctx, account, encrypted, loginRoute)
	} else if err == nil {
		var old []byte
		old, err = r.DB.AuthData(ctx, id)
		if err == nil {
			var ok bool
			ok, err = r.DB.ReplaceAccountCredentials(ctx, id, old, encrypted)
			if !ok && err == nil {
				err = errors.New("Credentials changed; retry sign-in.")
			}
		}
	}
	if err == nil {
		c := &accountConnection{state: domain.ConnectionAvailable}
		r.connections[id] = c
		err = r.installGmailLocked(id, cfg, c)
		if err == nil {
			account.Capabilities = r.providers[id].Capabilities()
		} else {
			c.state = domain.ConnectionCredentialsUnavailable
		}
	}
	r.mu.Unlock()
	release()
	if err == nil {
		r.connectionChanged(id)
		_ = r.Scheduler.Start(id, 0)
	}
	return account, err
}
