package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func microsoftTestPolicy() MicrosoftPolicy {
	return MicrosoftPolicy{ClientID: "11111111-2222-3333-4444-555555555555", Tenant: "common", Audience: "all", Graph: true, IMAP: true, SMTP: true}
}

type microsoftRoundTrip func(*http.Request) (*http.Response, error)

func (f microsoftRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func microsoftFixtureClient(t *testing.T, h http.HandlerFunc) *http.Client {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	origin, _ := url.Parse(s.URL)
	base := s.Client().Transport
	return &http.Client{Transport: microsoftRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || (r.URL.Host != "login.microsoftonline.com" && r.URL.Host != "graph.microsoft.com") {
			return nil, errors.New("fixture forbids external endpoint")
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = origin.Scheme
		u.Host = origin.Host
		copy.URL = &u
		copy.Host = origin.Host
		return base.RoundTrip(copy)
	})}
}
func microsoftExpired() MicrosoftCredentials {
	p := microsoftTestPolicy()
	g, _ := NewMicrosoftConfig(p, MicrosoftGraph)
	e, _ := NewMicrosoftConfig(p, MicrosoftExchange)
	return MicrosoftCredentials{Generation: 5, Grants: map[MicrosoftResource]MicrosoftGrant{
		MicrosoftGraph:    {AccessToken: "old-graph", RefreshToken: "graph-refresh", TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour), Scopes: g.Scopes},
		MicrosoftExchange: {AccessToken: "old-exchange", RefreshToken: "exchange-refresh", TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour), Scopes: e.Scopes},
	}}
}
func TestMicrosoftTenantAudienceAndResources(t *testing.T) {
	for _, tenant := range []string{"https://evil.test", "common/../consumers", " common", "COMMON", "%63ommon", "00000000-0000-0000-0000-000000000000", "tenant.example", "common\n"} {
		p := microsoftTestPolicy()
		p.Tenant = tenant
		if p.Validate() == nil {
			t.Errorf("accepted tenant %q", tenant)
		}
	}
	for _, pair := range [][2]string{{"common", "single_tenant"}, {"common", "organizations"}, {"organizations", "personal"}, {"consumers", "organizations"}, {"common", "unknown"}} {
		p := microsoftTestPolicy()
		p.Tenant, p.Audience = pair[0], pair[1]
		if p.Validate() == nil {
			t.Errorf("accepted mismatch %v", pair)
		}
	}
	p := microsoftTestPolicy()
	p.SMTP = false
	c, err := NewMicrosoftConfig(p, MicrosoftExchange)
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientSecret != "" || strings.Contains(strings.Join(c.Scopes, " "), "SMTP") || strings.Contains(strings.Join(c.Scopes, " "), "graph.microsoft") {
		t.Fatal("resource leakage")
	}
}
func TestMicrosoftAuthorizePKCEPublicClientAndIdentity(t *testing.T) {
	var challenge string
	var requests atomic.Int32
	client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/me" {
			if r.Header.Get("Authorization") != "Bearer graph-access" {
				t.Error("missing bearer")
			}
			_, _ = io.WriteString(w, `{"id":"stable-provider-user","mail":"Test@example.test","userPrincipalName":"untrusted@example.test"}`)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f := r.PostForm
		if f.Get("client_secret") != "" || f.Get("grant_type") != "authorization_code" || f.Get("code") != "fixture-code" {
			t.Error("incorrect public client exchange")
		}
		digest := sha256.Sum256([]byte(f.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
			t.Error("PKCE mismatch")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "graph-access", "refresh_token": "graph-refresh", "token_type": "Bearer", "expires_in": 3600, "scope": MicrosoftUserRead + " " + MicrosoftMailReadWrite + " " + MicrosoftMailSend})
	})
	cfg, _ := NewMicrosoftConfig(microsoftTestPolicy(), MicrosoftGraph)
	cfg.ClientSecret = "must-not-send"
	token, err := AuthorizeMicrosoft(context.Background(), cfg, client, func(raw string) error {
		u, _ := url.Parse(raw)
		q := u.Query()
		challenge = q.Get("code_challenge")
		if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "" || q.Get("prompt") != "" {
			t.Error("incorrect authorize parameters")
		}
		callback, _ := url.Parse(q.Get("redirect_uri"))
		values := url.Values{"state": {"wrong"}, "code": {"ignored"}}
		callback.RawQuery = values.Encode()
		res, e := http.Get(callback.String())
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Error("state mismatch accepted")
		}
		values.Set("state", q.Get("state"))
		values.Set("code", "fixture-code")
		callback.RawQuery = values.Encode()
		res, e = http.Get(callback.String())
		if e == nil {
			res.Body.Close()
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := MicrosoftProfile(context.Background(), client, token.AccessToken)
	if err != nil || id.Subject != "stable-provider-user" || id.Mailbox != "test@example.test" {
		t.Fatalf("identity %v %v", id, err)
	}
	if requests.Load() != 2 {
		t.Fatal("unexpected exchanges")
	}
}
func TestMicrosoftAuthorizeCancelDenyAndScopeReduction(t *testing.T) {
	client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"sensitive","refresh_token":"secret","token_type":"Bearer","expires_in":3600,"scope":"User.Read"}`)
	})
	cfg, _ := NewMicrosoftConfig(microsoftTestPolicy(), MicrosoftGraph)
	for _, mode := range []string{"cancel", "deny", "reduced"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := AuthorizeMicrosoft(ctx, cfg, client, func(raw string) error {
				u, _ := url.Parse(raw)
				q := u.Query()
				if mode == "cancel" {
					cancel()
					return nil
				}
				cb, _ := url.Parse(q.Get("redirect_uri"))
				v := url.Values{"state": {q.Get("state")}}
				if mode == "deny" {
					v.Set("error", "access_denied")
				} else {
					v.Set("code", "fixture")
				}
				cb.RawQuery = v.Encode()
				res, e := http.Get(cb.String())
				if e == nil {
					res.Body.Close()
				}
				return e
			})
			if err == nil || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe result %v", err)
			}
		})
	}
}
func TestMicrosoftRefreshSerializedResourceScopedPersistBeforeExpose(t *testing.T) {
	var requests atomic.Int32
	var saved atomic.Int32
	var mu sync.Mutex
	forms := map[string]int{}
	client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = r.ParseForm()
		scope := r.Form.Get("scope")
		rt := r.Form.Get("refresh_token")
		if r.Form.Get("client_secret") != "" || r.Form.Get("grant_type") != "refresh_token" {
			t.Error("bad refresh form")
		}
		if (rt == "graph-refresh") != strings.Contains(scope, "graph.microsoft.com") {
			t.Error("mixed resource")
		}
		mu.Lock()
		forms[rt]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": rt + "-new", "refresh_token": rt + "-rotated", "token_type": "Bearer", "expires_in": 3600, "scope": scope})
	})
	initial := microsoftExpired()
	disk := cloneMicrosoftCredentials(initial)
	tokens, err := NewMicrosoftTokens(microsoftTestPolicy(), client, initial, func(ctx context.Context, old, next MicrosoftCredentials) error {
		if !reflect.DeepEqual(old, disk) {
			t.Error("CAS old changed")
		}
		disk = cloneMicrosoftCredentials(next)
		saved.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("restore did network")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := MicrosoftGraph
			required := MicrosoftUserRead
			if i%2 == 0 {
				r = MicrosoftExchange
				required = MicrosoftIMAP
			}
			access, e := tokens.Token(context.Background(), r, required)
			if e != nil || access == "" || saved.Load() == 0 {
				t.Errorf("token exposed before persisted: %v", e)
			}
		}(i)
	}
	wg.Wait()
	if requests.Load() != 2 || saved.Load() != 2 || forms["graph-refresh"] != 1 || forms["exchange-refresh"] != 1 {
		t.Fatalf("refreshes %d saved %d forms %v", requests.Load(), saved.Load(), forms)
	}
	reopened, e := NewMicrosoftTokens(microsoftTestPolicy(), client, disk, func(context.Context, MicrosoftCredentials, MicrosoftCredentials) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	_, e = reopened.Token(context.Background(), MicrosoftGraph, MicrosoftUserRead)
	if e != nil || requests.Load() != 2 {
		t.Fatal("reopen refreshed unnecessarily")
	}
}
func TestMicrosoftRefreshCASLossNeverExposesOrRetries(t *testing.T) {
	for _, mode := range []string{"save-fault", "cas-loss"} {
		t.Run(mode, func(t *testing.T) {
			var count atomic.Int32
			client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				_ = r.ParseForm()
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "never-exposed", "refresh_token": "rotated", "token_type": "Bearer", "expires_in": 3600, "scope": r.Form.Get("scope")})
			})
			initial := microsoftExpired()
			source, _ := NewMicrosoftTokens(microsoftTestPolicy(), client, initial, func(context.Context, MicrosoftCredentials, MicrosoftCredentials) error {
				return errors.New(mode + " secret")
			})
			for i := 0; i < 2; i++ {
				access, err := source.Token(context.Background(), MicrosoftGraph)
				if access != "" || err == nil || strings.Contains(err.Error(), "secret") {
					t.Fatalf("exposed failed persistence %q %v", access, err)
				}
			}
			if count.Load() != 1 || !reflect.DeepEqual(initial, source.Snapshot()) {
				t.Fatal("lost CAS changed state or retried")
			}
		})
	}
}
func TestMicrosoftRefreshOmittedScopeAndRefreshPreservedIndependentPermissions(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		t.Run(map[bool]string{false: "omitted", true: "reduced"}[reduce], func(t *testing.T) {
			client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body := map[string]any{"access_token": "new", "token_type": "Bearer", "expires_in": 3600}
				if reduce {
					body["scope"] = MicrosoftIMAP
				}
				_ = json.NewEncoder(w).Encode(body)
			})
			initial := microsoftExpired()
			source, _ := NewMicrosoftTokens(microsoftTestPolicy(), client, initial, func(context.Context, MicrosoftCredentials, MicrosoftCredentials) error { return nil })
			access, err := source.Token(context.Background(), MicrosoftExchange, MicrosoftIMAP)
			if err != nil || access != "new" {
				t.Fatalf("healthy IMAP blocked %v", err)
			}
			if source.Snapshot().Grants[MicrosoftExchange].RefreshToken != "exchange-refresh" {
				t.Fatal("omitted RT lost")
			}
			access, err = source.Token(context.Background(), MicrosoftExchange, MicrosoftSMTP)
			if reduce && (access != "" || err == nil) {
				t.Fatal("SMTP scope reduction ignored")
			}
			if !reduce && err != nil {
				t.Fatal(err)
			}
			if access, err = source.Token(context.Background(), MicrosoftExchange, MicrosoftUserRead); access != "" || err == nil {
				t.Fatal("cross-resource scope accepted")
			}
		})
	}
}
func TestMicrosoftXOAUTH2ExactFramingAndBounds(t *testing.T) {
	client, err := NewMicrosoftXOAUTH2("a@example.test", "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	mechanism, initial, err := client.Start()
	if err != nil || mechanism != "XOAUTH2" || string(initial) != "user=a@example.test\x01auth=Bearer fixture-token\x01\x01" {
		t.Fatalf("wrong mechanism/framing %q", mechanism)
	}
	if reply, err := client.Next([]byte(`{"status":"401"}`)); err != nil || len(reply) != 0 {
		t.Fatal("missing empty auth failure response")
	}
	if _, err := client.Next(nil); err == nil {
		t.Fatal("unbounded challenges")
	}
	if _, _, err := client.Start(); err == nil {
		t.Fatal("reused SASL client")
	}
	for _, values := range [][2]string{{"a@example.test\x01evil", "token"}, {"a@example.test", "token\r\n"}, {"Name <a@example.test>", "token"}, {"a@example.test", ""}} {
		if _, err := NewMicrosoftXOAUTH2(values[0], values[1]); err == nil {
			t.Fatal("accepted injection")
		}
	}
}
func TestMicrosoftInvalidTokenResponsesSanitized(t *testing.T) {
	for _, body := range []string{`{"access_token":"secret","refresh_token":"secret","token_type":"Bearer","expires_in":0}`, `{"access_token":"secret","refresh_token":"secret","token_type":"mac","expires_in":3600}`, `{"error":"invalid_grant","error_description":"secret"}`, strings.Repeat("secret", 12000)} {
		client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(body, "invalid_grant") {
				w.WriteHeader(400)
			}
			_, _ = io.WriteString(w, body)
		})
		source, _ := NewMicrosoftTokens(microsoftTestPolicy(), client, microsoftExpired(), func(context.Context, MicrosoftCredentials, MicrosoftCredentials) error {
			t.Error("invalid token persisted")
			return nil
		})
		access, err := source.Token(context.Background(), MicrosoftGraph)
		if access != "" || err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe token result %q %v", access, err)
		}
	}
}
func TestMicrosoftHTTPRedirectRejected(t *testing.T) {
	var calls atomic.Int32
	client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "https://graph.microsoft.com/steal")
		w.WriteHeader(307)
	})
	_, err := MicrosoftProfile(context.Background(), client, "fixture")
	if err == nil || calls.Load() != 1 {
		t.Fatal("bearer redirect followed")
	}
}

func TestMicrosoftSharedRefreshFamilyRotationPreservesResourceProvenance(t *testing.T) {
	initial := microsoftExpired()
	exchange := initial.Grants[MicrosoftExchange]
	exchange.RefreshToken = "graph-refresh"
	initial.Grants[MicrosoftExchange] = exchange
	client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-graph", "refresh_token": "family-rotated", "token_type": "Bearer", "expires_in": 3600, "scope": r.Form.Get("scope")})
	})
	source, _ := NewMicrosoftTokens(microsoftTestPolicy(), client, initial, func(context.Context, MicrosoftCredentials, MicrosoftCredentials) error { return nil })
	if _, err := source.Token(context.Background(), MicrosoftGraph); err != nil {
		t.Fatal(err)
	}
	after := source.Snapshot()
	if after.Grants[MicrosoftExchange].RefreshToken != "family-rotated" || !reflect.DeepEqual(after.Grants[MicrosoftExchange].Scopes, exchange.Scopes) || after.Grants[MicrosoftExchange].AccessToken != exchange.AccessToken {
		t.Fatal("shared refresh rotation crossed resource access/scope provenance")
	}
}

func TestMicrosoftExchangeIdentityUsesItsOwnRefreshAndRetainsRotation(t *testing.T) {
	var calls atomic.Int32
	client := microsoftFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "exchange-original" || r.Form.Get("scope") != "offline_access "+MicrosoftUserRead {
				t.Error("identity proof did not use explicit Graph scope and Exchange credential")
			}
			_, _ = io.WriteString(w, `{"access_token":"identity","refresh_token":"exchange-rotated","token_type":"Bearer","expires_in":3600,"scope":"User.Read"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer identity" {
			t.Error("profile used wrong resource")
		}
		_, _ = io.WriteString(w, `{"id":"stable-subject","mail":"a@example.test"}`)
	})
	exchange := &oauth2.Token{AccessToken: "exchange-access", RefreshToken: "exchange-original", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}
	id, updated, err := VerifyMicrosoftExchangeIdentity(context.Background(), microsoftTestPolicy(), client, exchange)
	if err != nil {
		t.Fatal(err)
	}
	if id.Subject != "stable-subject" || updated.AccessToken != "exchange-access" || updated.RefreshToken != "exchange-rotated" || exchange.RefreshToken != "exchange-original" || calls.Load() != 2 {
		t.Fatal("identity/rotation changed resource access token or original")
	}
}
