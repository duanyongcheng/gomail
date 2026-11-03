package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/oauth"
	"golang.org/x/oauth2"
)

type microsoftServiceRoundTrip func(*http.Request) (*http.Response, error)

func (f microsoftServiceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func microsoftServiceFixture(t *testing.T) (*Runtime, *MicrosoftService, *string, *int) {
	t.Helper()
	r, _, _ := protocolAccountFixture(t)
	subject := "stable-subject"
	calls := 0
	r.RegisterProviderFactory("outlook", func(pc ProviderContext) (domain.Transport, error) { return &protocolFixture{context: pc}, nil })
	s := &MicrosoftService{Runtime: r, HTTP: &http.Client{Transport: microsoftServiceRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "graph.microsoft.com" && req.URL.Host != "login.microsoftonline.com" {
			return nil, errors.New("fixture forbids live request")
		}
		body := `{"id":"` + subject + `","mail":"mailbox@example.invalid"}`
		if req.Method == "POST" {
			_ = req.ParseForm()
			data, _ := json.Marshal(map[string]any{"access_token": "identity-access", "refresh_token": "exchange-rotated", "token_type": "Bearer", "expires_in": 3600, "scope": oauth.MicrosoftUserRead})
			body = string(data)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, AuthorizeFlow: func(ctx context.Context, cfg *oauth2.Config, _ *http.Client, _ func(string) error) (*oauth2.Token, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return (&oauth2.Token{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}).WithExtra(map[string]any{"scope": strings.Join(cfg.Scopes, " ")}), nil
	}}
	return r, s, &subject, &calls
}
func microsoftServiceRequest() MicrosoftAuthorizeRequest {
	return MicrosoftAuthorizeRequest{ClientID: "11111111-2222-3333-4444-555555555555", Tenant: "common", Audience: "all", Resource: "graph", Graph: true, DisplayName: "Synthetic Microsoft"}
}
func TestMicrosoftServiceVerifiedEncryptedAccountAndLazyRestore(t *testing.T) {
	r, s, _, calls := microsoftServiceFixture(t)
	request := microsoftServiceRequest()
	a, err := s.Authorize(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "mailbox@example.invalid" || a.Provider != "outlook" {
		t.Fatal("identity was not verified", a)
	}
	encrypted, err := r.DB.AuthData(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("fixture-refresh")) || bytes.Contains(encrypted, []byte("fixture-access")) {
		t.Fatal("plaintext credentials")
	}
	status, err := s.Status(t.Context(), AccountRequest{AccountID: a.ID})
	if err != nil || !status.GraphAuthorized || status.IMAPAuthorized || status.SMTPAuthorized {
		t.Fatalf("status %v %v", status, err)
	}
	encoded, _ := json.Marshal(status)
	if bytes.Contains(encoded, []byte("fixture")) {
		t.Fatal("secret status")
	}
	before := *calls
	if err = r.Restore(); err != nil {
		t.Fatal(err)
	}
	if *calls != before {
		t.Fatal("restore did network")
	}
	if _, err = s.Authorize(t.Context(), request); err == nil {
		t.Fatal("duplicate add replaced saved account")
	}
}
func TestMicrosoftServiceWrongMailboxCancellationAndCASRetainSavedCredentials(t *testing.T) {
	for _, mode := range []string{"wrong-mailbox", "cancel", "cas-conflict"} {
		t.Run(mode, func(t *testing.T) {
			r, s, subject, _ := microsoftServiceFixture(t)
			request := microsoftServiceRequest()
			a, err := s.Authorize(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			old, _ := r.DB.AuthData(t.Context(), a.ID)
			request.AccountID = a.ID
			switch mode {
			case "wrong-mailbox":
				*subject = "another-subject"
			case "cancel":
				s.AuthorizeFlow = func(ctx context.Context, _ *oauth2.Config, _ *http.Client, _ func(string) error) (*oauth2.Token, error) {
					_, _ = s.Cancel(ctx, AccountRequest{AccountID: a.ID})
					return nil, ctx.Err()
				}
			case "cas-conflict":
				flow := s.AuthorizeFlow
				s.AuthorizeFlow = func(ctx context.Context, cfg *oauth2.Config, c *http.Client, open func(string) error) (*oauth2.Token, error) {
					r.mu.RLock()
					saved, _, e := r.loadProtocol(ctx, a.ID)
					r.mu.RUnlock()
					if e != nil {
						return nil, e
					}
					next, e := r.encodeProtocol(ctx, a.ID, saved)
					if e != nil {
						return nil, e
					}
					ok, e := r.DB.CompareAndSwapAuthData(ctx, a.ID, old, next, time.Now().Unix())
					if e != nil || !ok {
						return nil, errors.New("fixture replacement failed")
					}
					return flow(ctx, cfg, c, open)
				}
			}
			if _, err = s.Authorize(t.Context(), request); err == nil {
				t.Fatal("unsafe replacement succeeded")
			}
			after, _ := r.DB.AuthData(t.Context(), a.ID)
			if mode != "cas-conflict" && !bytes.Equal(old, after) {
				t.Fatal("failed replacement changed credentials")
			}
			account, err := r.DB.Account(t.Context(), a.ID)
			if err != nil || account.Email != a.Email {
				t.Fatal("account changed")
			}
		})
	}
}
func TestMicrosoftServiceExchangeIdentityAndIndependentModes(t *testing.T) {
	r, s, _, _ := microsoftServiceFixture(t)
	request := microsoftServiceRequest()
	request.Resource = "exchange"
	request.Graph = false
	request.IMAP = true
	request.SMTP = false
	a, err := s.Authorize(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(t.Context(), AccountRequest{AccountID: a.ID})
	if err != nil || !status.IMAPAuthorized || status.SMTPAuthorized || status.GraphAuthorized {
		t.Fatalf("mode status %v %v", status, err)
	}
	r.mu.RLock()
	config, _, err := r.loadProtocol(t.Context(), a.ID)
	r.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if config.SMTP.AuthMode != "disabled" || config.IMAP.AuthMode != "xoauth2" || config.Microsoft.Grants["exchange"].RefreshToken != "exchange-rotated" {
		t.Fatal("incorrect protocol auth/rotation")
	}
}
