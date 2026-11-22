package oauth

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/crypto"
	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"golang.org/x/oauth2"
)

func TestClientConfigPrecedenceAndScope(t *testing.T) {
	for _, name := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"} {
		old, exists := os.LookupEnv(name)
		_ = os.Unsetenv(name)
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(name, old)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
	if Google().ClientID != "" || Google().ClientSecret != "" || GoogleFallbackSource() != "none" {
		t.Fatal("unexpected non-empty default Google OAuth credentials")
	}
	t.Setenv("GOOGLE_CLIENT_ID", " process-client ")
	t.Setenv("GOOGLE_CLIENT_SECRET", " process-secret ")
	cfg := Google()
	if cfg.ClientID != "process-client" || cfg.ClientSecret != "process-secret" || GoogleFallbackSource() != "env" {
		t.Fatal("environment credentials not loaded")
	}
	if len(cfg.Scopes) != 4 || cfg.Scopes[0] != ModifyScope {
		t.Fatal("wrong Gmail scopes")
	}
	t.Setenv("GOOGLE_CLIENT_ID", "")
	if Google().ClientID != "" || GoogleFallbackSource() != "none" {
		t.Fatal("explicit empty client did not clear Google OAuth config")
	}
	explicit := GoogleWithCredentials(" app-client ", " app-secret ")
	if explicit.ClientID != "app-client" || explicit.ClientSecret != "app-secret" || len(explicit.Scopes) != 4 || explicit.Scopes[0] != ModifyScope {
		t.Fatal("explicit Google credentials helper mismatch")
	}
}

func TestDatabaseVaultRestoreAndCAS(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := crypto.FromKey(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	token := (&oauth2.Token{AccessToken: "private-access", RefreshToken: "private-refresh", Expiry: time.Now().Add(time.Hour)}).WithExtra(map[string]any{"scope": ModifyScope})
	encrypted, err := EncodeToken(service, "a", "client", token)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("private")) {
		t.Fatal("plaintext persisted")
	}
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, encrypted); err != nil {
		t.Fatal(err)
	}
	vault := &DatabaseVault{DB: db, Crypto: service, Context: t.Context(), AccountID: "a", ClientID: "client"}
	stale := &DatabaseVault{DB: db, Crypto: service, Context: t.Context(), AccountID: "a", ClientID: "client"}
	for _, v := range []*DatabaseVault{vault, stale} {
		restored, err := v.Load()
		if err != nil || restored.RefreshToken != token.RefreshToken || restored.Extra("scope") != ModifyScope {
			t.Fatal("restore failed", err)
		}
	}
	token.AccessToken = "new-access"
	if err := vault.Save(token); err != nil {
		t.Fatal(err)
	}
	if err := stale.Save(token); err == nil {
		t.Fatal("stale refresh overwrote token")
	}
	wrong := &DatabaseVault{DB: db, Crypto: service, Context: t.Context(), AccountID: "a", ClientID: "different-client"}
	if _, err := wrong.Load(); err == nil {
		t.Fatal("wrong OAuth client accepted")
	}
}
