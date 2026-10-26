package gmail

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const ReadScope = "https://www.googleapis.com/auth/gmail.readonly"
const ModifyScope = "https://www.googleapis.com/auth/gmail.modify"

// LoadDesktop accepts only explicit installed-app JSON. Endpoints from the file
// are ignored to prevent sending authorization codes/secrets to an arbitrary URL.
func LoadDesktop(path string, modify bool) (*oauth2.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read selected OAuth configuration")
	}
	if len(data) > 64<<10 {
		return nil, errors.New("OAuth configuration too large")
	}
	var file struct {
		Installed *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
	}
	if json.Unmarshal(data, &file) != nil || file.Installed == nil || file.Installed.ClientID == "" {
		return nil, errors.New("Google Desktop installed-app JSON required")
	}
	scope := ReadScope
	if modify {
		scope = ModifyScope
	}
	return &oauth2.Config{ClientID: file.Installed.ClientID, ClientSecret: file.Installed.ClientSecret, Scopes: []string{scope}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams}}, nil
}

// Authorize opens only the system browser, listens on a one-shot loopback port,
// and exchanges a validated callback with the same PKCE verifier.
func Authorize(ctx context.Context, cfg *oauth2.Config, client *http.Client, open func(string) error) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	copyCfg := *cfg
	copyCfg.RedirectURL = "http://" + ln.Addr().String() + "/oauth/callback"
	state := oauth2.GenerateVerifier()
	verifier := oauth2.GenerateVerifier()
	type response struct {
		code string
		err  error
	}
	result := make(chan response, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet || r.Host != ln.Addr().String() || len(r.URL.RawQuery) > 8192 {
			http.Error(w, "Invalid callback", 400)
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid state", 400)
			return
		}
		if q.Get("error") != "" {
			once.Do(func() { result <- response{err: errors.New("Google authorization denied")} })
			http.Error(w, "Authorization denied", 400)
			return
		}
		if len(q["code"]) != 1 || q.Get("code") == "" {
			http.Error(w, "Missing authorization code", 400)
			return
		}
		used := false
		once.Do(func() { used = true; result <- response{code: q.Get("code")} })
		if !used {
			http.Error(w, "Callback already consumed", 409)
			return
		}
		_, _ = io.WriteString(w, "Authorization received. Return to Pebble; token exchange must still finish.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	defer server.Close()
	go func() { _ = server.Serve(ln) }()
	authURL := copyCfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"), oauth2.S256ChallengeOption(verifier))
	if err = open(authURL); err != nil {
		return nil, errors.New("could not open OAuth browser")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case callback := <-result:
		if callback.err != nil {
			return nil, callback.err
		}
		token, err := copyCfg.Exchange(context.WithValue(ctx, oauth2.HTTPClient, client), callback.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, errors.New("OAuth code exchange failed; retry browser authorization")
		}
		if token.AccessToken == "" || token.RefreshToken == "" {
			return nil, errors.New("OAuth offline grant did not return a refresh token; reauthorize with consent")
		}
		return token, nil
	}
}

type TokenVault interface {
	Save(*oauth2.Token) error
	Load() (*oauth2.Token, error)
}

// Tokens serializes refresh and persists a new token before exposing it to API.
type Tokens struct {
	mu    sync.Mutex
	cfg   *oauth2.Config
	http  *http.Client
	vault TokenVault
	token *oauth2.Token
}

func NewTokens(cfg *oauth2.Config, client *http.Client, vault TokenVault) *Tokens {
	return &Tokens{cfg: cfg, http: client, vault: vault}
}
func (t *Tokens) Install(token *oauth2.Token) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.vault.Save(token); err != nil {
		return err
	}
	copy := *token
	t.token = &copy
	return nil
}
func (t *Tokens) Invalidate(access string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != nil && t.token.AccessToken == access {
		t.token.Expiry = time.Now().Add(-time.Hour)
	}
}
func (t *Tokens) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token == nil {
		token, err := t.vault.Load()
		if err != nil {
			return "", errors.New("Gmail sign-in required")
		}
		t.token = token
	}
	if t.token.AccessToken != "" && (t.token.Expiry.IsZero() || time.Until(t.token.Expiry) > time.Minute) {
		return t.token.AccessToken, nil
	}
	if t.token.RefreshToken == "" {
		return "", errors.New("Gmail reauthorization required")
	}
	old := *t.token
	expired := old
	expired.Expiry = time.Now().Add(-time.Hour)
	fresh, err := t.cfg.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, t.http), &expired).Token()
	if err != nil {
		return "", errors.New("Gmail token refresh failed; reauthorization may be required")
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = old.RefreshToken
	}
	if err = t.vault.Save(fresh); err != nil {
		return "", fmt.Errorf("cannot persist refreshed token: %w", err)
	}
	t.token = fresh
	return fresh.AccessToken, nil
}
