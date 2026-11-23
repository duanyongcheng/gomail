package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/crypto"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/oauth"
	"github.com/duanyongcheng/gomail/internal/store"
	"github.com/duanyongcheng/gomail/internal/testgmail"
	"golang.org/x/oauth2"
)

type failingKeyring struct{ err error }

func (f failingKeyring) Get(string) ([]byte, error)     { return nil, f.err }
func (f failingKeyring) Set(string, []byte) error       { return f.err }

func TestGoogleOAuthConfigSaveImmediateEffectPrecedenceAndReopen(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")

	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ring := &testKeyring{key: bytes.Repeat([]byte{7}, 32)}
	r := New(t.Context(), db, ring, func(string) error { return nil })
	s := &AccountService{Runtime: r}

	if status := s.Status(Empty{}); status.Configured {
		t.Fatalf("expected unconfigured initially, got %+v", status)
	}
	cfg, err := s.GoogleOAuthConfig(t.Context(), Empty{})
	if err != nil || cfg.Configured || cfg.Source != "none" || cfg.HasAppConfig {
		t.Fatalf("initial config = (%+v, %v)", cfg, err)
	}

	// Reject invalid inputs before writing anything.
	for _, bad := range []SaveGoogleOAuthConfigRequest{
		{ClientID: "   "},
		{ClientID: "bad client id"},
		{ClientID: "ok-id", ClientSecret: "bad\nsecret"},
	} {
		if _, err := s.SaveGoogleOAuthConfig(t.Context(), bad); err == nil {
			t.Fatalf("expected validation failure for %+v", bad)
		}
	}

	// Save in-app config and verify immediate effect + ciphertext at rest.
	saved, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:     " app-client-1.apps.googleusercontent.com ",
		ClientSecret: " super-secret-value ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Configured || saved.Source != "app" || saved.ClientID != "app-client-1.apps.googleusercontent.com" || !saved.HasSecret || !saved.HasAppConfig || !saved.AppHasSecret {
		t.Fatalf("unexpected saved config: %+v", saved)
	}
	if status := s.Status(Empty{}); !status.Configured {
		t.Fatalf("Status did not reflect saved config immediately: %+v", status)
	}
	raw, err := db.GoogleOAuthSettings(t.Context())
	if err != nil || len(raw) == 0 {
		t.Fatalf("missing stored blob: (%d, %v)", len(raw), err)
	}
	if bytes.Contains(raw, []byte("super-secret-value")) || bytes.Contains(raw, []byte("app-client-1")) {
		t.Fatal("Google OAuth settings stored in plaintext")
	}

	// Keep existing secret when secret input is empty and KeepExistingSecret=true.
	kept, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:           "app-client-1.apps.googleusercontent.com",
		ClientSecret:       "",
		KeepExistingSecret: true,
	})
	if err != nil || !kept.HasSecret || !kept.AppHasSecret {
		t.Fatalf("secret not preserved: (%+v, %v)", kept, err)
	}
	if got := r.googleOAuthConfig(); got.ClientSecret != "super-secret-value" {
		t.Fatalf("effective secret = %q, want super-secret-value", got.ClientSecret)
	}

	// In-app config takes precedence over environment variables.
	t.Setenv("GOOGLE_CLIENT_ID", "env-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "env-secret")
	if got := r.googleOAuthConfig(); got.ClientID != "app-client-1.apps.googleusercontent.com" || got.ClientSecret != "super-secret-value" {
		t.Fatalf("app config did not override env: %+v", got)
	}

	// Explicit secret clearing works when KeepExistingSecret=false.
	clearedSecret, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:           "app-client-1.apps.googleusercontent.com",
		ClientSecret:       "",
		KeepExistingSecret: false,
	})
	if err != nil || clearedSecret.HasSecret || clearedSecret.AppHasSecret {
		t.Fatalf("expected secret cleared: (%+v, %v)", clearedSecret, err)
	}

	// Re-save with secret and verify persistence across a fresh Runtime + Restore.
	if _, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:     "app-client-reopen",
		ClientSecret: "reopen-secret",
	}); err != nil {
		t.Fatal(err)
	}
	reopened := New(t.Context(), db, ring, func(string) error { return nil })
	if err := reopened.Restore(); err != nil {
		t.Fatal(err)
	}
	if got := reopened.googleOAuthConfig(); got.ClientID != "app-client-reopen" || got.ClientSecret != "reopen-secret" {
		t.Fatalf("reopened config mismatch: %+v", got)
	}

	// Locked keyring fails closed and does not silently fall back to env or overwrite DEK.
	lockedRuntime := New(t.Context(), db, failingKeyring{err: errors.New("locked")}, func(string) error { return nil })
	if err := lockedRuntime.Restore(); err == nil {
		t.Fatal("expected Restore error when keyring is locked with saved OAuth settings")
	}
	lockedSvc := &AccountService{Runtime: lockedRuntime}
	if status := lockedSvc.Status(Empty{}); status.Configured {
		t.Fatalf("locked runtime unexpectedly reported configured: %+v", status)
	}
	lockedView, err := lockedSvc.GoogleOAuthConfig(t.Context(), Empty{})
	if err != nil || !lockedView.KeyringError || lockedView.Configured || !lockedView.HasAppConfig {
		t.Fatalf("unexpected locked projection: (%+v, %v)", lockedView, err)
	}
	if _, err := lockedSvc.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{ClientID: "overwrite"}); err == nil {
		t.Fatal("expected Save to fail when keyring is locked")
	}

	// Clearing app config falls back to environment variables immediately.
	afterClear, err := (&AccountService{Runtime: reopened}).ClearGoogleOAuthConfig(t.Context(), Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if !afterClear.Configured || afterClear.Source != "env" || afterClear.ClientID != "env-client" || !afterClear.HasSecret || afterClear.HasAppConfig {
		t.Fatalf("expected fallback to env after clear, got %+v", afterClear)
	}
}

func TestGoogleOAuthConfigHotReloadsGmailAccountsAndLogin(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")

	fixture := testgmail.NewMock()
	t.Cleanup(fixture.Close)
	fixture.ProfileEmail = "demo@example.invalid"
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ring := &testKeyring{key: bytes.Repeat([]byte{7}, 32)}
	r := New(t.Context(), db, ring, func(string) error { return nil })
	r.GmailAPIBase = fixture.Server.URL + "/gmail/v1/users/me"
	var seenClientID, seenClientSecret string
	r.Authorize = func(_ context.Context, cfg *oauth2.Config, _ *http.Client, _ func(string) error) (*oauth2.Token, error) {
		seenClientID = cfg.ClientID
		seenClientSecret = cfg.ClientSecret
		return (&oauth2.Token{
			AccessToken:  "demo-access",
			RefreshToken: "demo-refresh",
			Expiry:       time.Now().Add(time.Hour),
		}).WithExtra(map[string]any{"scope": oauth.ModifyScope}), nil
	}
	s := &AccountService{Runtime: r}

	// Seed an existing Gmail account issued by client-A while unconfigured at startup.
	key := bytes.Repeat([]byte{9}, 32)
	if err := ring.Set(crypto.KeyringAccount, key); err != nil {
		t.Fatal(err)
	}
	cipher, err := crypto.FromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	// Login uses sha256(email)[:16] for ID; use that deterministic ID for fixture profile.
	if _, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:     "client-A",
		ClientSecret: "secret-A1",
	}); err != nil {
		t.Fatal(err)
	}
	account, err := s.Login(t.Context(), LoginRequest{Provider: "gmail"})
	if err != nil {
		t.Fatal(err)
	}
	if seenClientID != "client-A" || seenClientSecret != "secret-A1" {
		t.Fatalf("login used (%q, %q), want (client-A, secret-A1)", seenClientID, seenClientSecret)
	}
	origAuth, err := db.AuthData(t.Context(), account.ID)
	if err != nil || len(origAuth) == 0 {
		t.Fatal("missing account auth data", err)
	}

	// Clear config -> account immediately enters configuration_required.
	if _, err := s.ClearGoogleOAuthConfig(t.Context(), Empty{}); err != nil {
		t.Fatal(err)
	}
	conns, err := s.Connections(t.Context(), Empty{})
	if err != nil || len(conns) != 1 || conns[0].State != domain.ConnectionConfigurationRequired || conns[0].CanReconnect {
		t.Fatalf("after clear connections = (%+v, %v)", conns, err)
	}

	// Save matching client-A with updated secret -> account immediately restores to available.
	if _, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:     "client-A",
		ClientSecret: "secret-A2",
	}); err != nil {
		t.Fatal(err)
	}
	conns, err = s.Connections(t.Context(), Empty{})
	if err != nil || len(conns) != 1 || conns[0].State != domain.ConnectionAvailable || !conns[0].CanReconnect {
		t.Fatalf("after restore save connections = (%+v, %v)", conns, err)
	}
	gen1 := r.connections[account.ID]

	// Update secret only -> provider generation replaced, still available.
	if _, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:     "client-A",
		ClientSecret: "secret-A3",
	}); err != nil {
		t.Fatal(err)
	}
	if r.connections[account.ID] == gen1 {
		t.Fatal("expected provider generation replacement on secret update")
	}

	// Change to different client-B -> account marked reauth_required, credentials preserved, reconnect uses client-B.
	if _, err := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{
		ClientID:     "client-B",
		ClientSecret: "secret-B",
	}); err != nil {
		t.Fatal(err)
	}
	conns, err = s.Connections(t.Context(), Empty{})
	if err != nil || len(conns) != 1 || conns[0].State != domain.ConnectionReauthRequired || !conns[0].CanReconnect {
		t.Fatalf("after client change connections = (%+v, %v)", conns, err)
	}
	preserved, err := db.AuthData(t.Context(), account.ID)
	if err != nil || !bytes.Equal(preserved, origAuth) {
		t.Fatal("changing client ID unexpectedly modified stored account token")
	}

	// Verify authFlowMu guards config mutation while sign-in is active.
	r.authFlowMu.Lock()
	_, busyErr := s.SaveGoogleOAuthConfig(t.Context(), SaveGoogleOAuthConfigRequest{ClientID: "client-C"})
	r.authFlowMu.Unlock()
	if busyErr == nil {
		t.Fatal("expected SaveGoogleOAuthConfig to fail while auth flow is active")
	}

	// Reconnect now authorizes using client-B / secret-B and restores account to available.
	if _, err := s.Reconnect(t.Context(), AccountRequest{AccountID: account.ID}); err != nil {
		t.Fatal(err)
	}
	if seenClientID != "client-B" || seenClientSecret != "secret-B" {
		t.Fatalf("reconnect used (%q, %q), want (client-B, secret-B)", seenClientID, seenClientSecret)
	}
	conns, err = s.Connections(t.Context(), Empty{})
	if err != nil || len(conns) != 1 || conns[0].State != domain.ConnectionAvailable {
		t.Fatalf("after reconnect connections = (%+v, %v)", conns, err)
	}
	_ = cipher
}
