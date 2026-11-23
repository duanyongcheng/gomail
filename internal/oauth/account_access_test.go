package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"golang.org/x/oauth2"
)

type memoryTokenVault struct {
	token *oauth2.Token
	saves int
}

func (v *memoryTokenVault) Load() (*oauth2.Token, error)   { return v.token, nil }
func (v *memoryTokenVault) Save(token *oauth2.Token) error { v.token = token; v.saves++; return nil }

func TestRefreshClassifiesAuthorizationAndTransientFailures(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		state      domain.ConnectionState
	}{
		{"revoked", `{"error":"invalid_grant","error_description":"private-token"}`, 400, domain.ConnectionReauthRequired},
		{"wrong client", `{"error":"invalid_client"}`, 401, domain.ConnectionConfigurationRequired},
		{"server", `{"error":"temporarily_unavailable"}`, 503, domain.ConnectionTemporaryError},
		{"quota", `{"error":"slow_down"}`, 429, domain.ConnectionTemporaryError},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			vault := &memoryTokenVault{token: &oauth2.Token{AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)}}
			cfg := &oauth2.Config{ClientID: "client", Endpoint: oauth2.Endpoint{TokenURL: server.URL, AuthStyle: oauth2.AuthStyleInParams}}
			_, err := NewTokens(cfg, server.Client(), vault).Token(t.Context())
			var access *domain.AccessError
			if !errors.As(err, &access) || access.State != test.state || strings.Contains(err.Error(), "private-token") {
				t.Fatal("wrong or unsafe error", err)
			}
			if vault.saves != 0 || vault.token.RefreshToken != "refresh" {
				t.Fatal("failed refresh overwrote credentials")
			}
		})
	}
}

func TestRefreshPreservesGrantScopeAndRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new","expires_in":3600}`))
	}))
	defer server.Close()
	old := (&oauth2.Token{RefreshToken: "original", Expiry: time.Now().Add(-time.Hour)}).WithExtra(map[string]any{"scope": ModifyScope})
	vault := &memoryTokenVault{token: old}
	cfg := &oauth2.Config{ClientID: "client", Endpoint: oauth2.Endpoint{TokenURL: server.URL, AuthStyle: oauth2.AuthStyleInParams}}
	if _, err := NewTokens(cfg, server.Client(), vault).Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if vault.token.RefreshToken != "original" || vault.token.Extra("scope") != ModifyScope || vault.saves != 1 {
		t.Fatal("grant metadata lost")
	}
}

func TestNewGrantRequiresActualMailWriteScope(t *testing.T) {
	for _, scope := range []string{"", ReadScope, "openid email profile", ModifyScope + ".other"} {
		token := (&oauth2.Token{AccessToken: "access", RefreshToken: "refresh"}).WithExtra(map[string]any{"scope": scope})
		if ValidateGmailGrant(token) == nil {
			t.Fatal("accepted insufficient grant", scope)
		}
	}
	for _, scope := range []string{ModifyScope, "openid " + ModifyScope + " email", "https://mail.google.com/"} {
		if err := ValidateGmailGrant((&oauth2.Token{AccessToken: "access", RefreshToken: "refresh"}).WithExtra(map[string]any{"scope": scope})); err != nil {
			t.Fatal(err)
		}
	}
}
