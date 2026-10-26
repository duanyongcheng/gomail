package gmail

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/mailcore"
	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/emersion/go-message/mail"
	"golang.org/x/oauth2"
)

func testToken(t *testing.T, m *Mock) *Tokens {
	t.Helper()
	vault := &Vault{Path: filepath.Join(t.TempDir(), "token.enc"), Key: []byte(strings.Repeat("x", 32)), AAD: "demo-client:modify"}
	tokens := NewTokens(m.OAuthConfig(), m.Server.Client(), vault)
	if err := tokens.Install(&oauth2.Token{AccessToken: "demo-access", RefreshToken: "demo-refresh", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return tokens
}
func setup(t *testing.T) (*Core, *Mock) {
	t.Helper()
	m := NewMock()
	t.Cleanup(m.Close)
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "mail.sqlite"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	schema, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(s) != "" {
			if _, err = db.Execute(context.Background(), s); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, err = db.Execute(context.Background(), "INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES(?,'demo','gmail',0,0)", AccountID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(context.Background(), db, m.API(testToken(t, m)), true)
	if err != nil {
		t.Fatal(err)
	}
	return c, m
}
func raw(t *testing.T, subject string) []byte {
	t.Helper()
	b, e := mailcore.Build("sender@example.invalid", subject, "中文搜索 Gmail OAuth test", "<b>Gmail HTML</b>", "test@example.invalid", []*mail.Address{{Address: "demo@example.invalid"}}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestOAuthPKCEAndStateValidation(t *testing.T) {
	m := NewMock()
	defer m.Close()
	token, err := Authorize(context.Background(), m.OAuthConfig(), m.Server.Client(), func(authURL string) error {
		u, _ := url.Parse(authURL)
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" {
			t.Fatal(q)
		}
		redirect, _ := url.Parse(q.Get("redirect_uri"))
		bad := *redirect
		bad.RawQuery = "state=wrong&code=attacker"
		resp, e := http.Get(bad.String())
		if e != nil {
			return e
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal("bad state accepted")
		}
		resp, e = http.Get(authURL)
		if e == nil {
			resp.Body.Close()
		}
		return e
	})
	if err != nil || token.RefreshToken != "demo-refresh" {
		t.Fatal(token, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = Authorize(ctx, m.OAuthConfig(), m.Server.Client(), func(string) error { return nil }); err == nil {
		t.Fatal("timeout ignored")
	}
}
func TestEncryptedTokenAndConcurrentRefresh(t *testing.T) {
	m := NewMock()
	defer m.Close()
	tokens := testToken(t, m)
	expired := &oauth2.Token{AccessToken: "expired", RefreshToken: "demo-refresh", Expiry: time.Now().Add(-time.Hour)}
	if err := tokens.Install(expired); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, e := tokens.Token(context.Background())
			if e != nil || token != "refreshed-access" {
				t.Error(token, e)
			}
		}()
	}
	wg.Wait()
	m.mu.Lock()
	n := m.Refreshes
	m.mu.Unlock()
	if n != 1 {
		t.Fatal("concurrent refresh count", n)
	}
	stored, err := tokens.vault.Load()
	if err != nil || stored.RefreshToken != "demo-refresh" {
		t.Fatal(stored, err)
	}
	v := tokens.vault.(*Vault)
	blob, _ := os.ReadFile(v.Path)
	if strings.Contains(string(blob), "demo-refresh") {
		t.Fatal("plaintext token")
	}
	v.AAD = "wrong"
	if _, err = v.Load(); err == nil {
		t.Fatal("AAD not bound")
	}
}
func TestGmailInitialHistoryLabelsFlagsAndFallback(t *testing.T) {
	c, m := setup(t)
	m.Set("m1", raw(t, "Gmail 中文一"), []string{"INBOX", "UNREAD", "Label_work"})
	m.Set("m2", raw(t, "Gmail 中文二"), []string{"INBOX"})
	result, err := c.Sync(context.Background())
	if err != nil || result.Count != 2 || !result.Initial {
		t.Fatal(result, err)
	}
	cursor, _ := c.Cursor(context.Background())
	if cursor != m.HistoryID {
		t.Fatal("opaque cursor lost precision", cursor)
	}
	truth := true
	if err = c.Flags(context.Background(), "gmail:m1", &truth, &truth); err != nil {
		t.Fatal(err)
	}
	rows, err := c.DB.Query(context.Background(), "SELECT is_read,is_starred FROM messages WHERE id='gmail:m1'")
	if err != nil || rows.Values[0][0] != int64(1) || rows.Values[0][1] != int64(1) {
		t.Fatal(rows, err)
	}
	m.Delete("m2")
	m.ChangeHistory("900719925474099312346")
	result, err = c.Sync(context.Background())
	if err != nil || result.Initial {
		t.Fatal(result, err)
	}
	hits, err := c.Search(context.Background(), "中文搜索", AccountID, 10)
	if err != nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
	m.mu.Lock()
	m.Expired = true
	m.mu.Unlock()
	result, err = c.Sync(context.Background())
	if err != nil || !result.Initial {
		t.Fatal("history 404 fallback", result, err)
	}
}
func TestFailureAndReadOnlySafety(t *testing.T) {
	c, m := setup(t)
	m.Set("m1", raw(t, "initial"), []string{"INBOX"})
	if _, err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := c.Cursor(context.Background())
	m.Set("m2", []byte("malformed body"), []string{"INBOX"})
	m.ChangeHistory("900719925474099312347")
	if _, err := c.Sync(context.Background()); err == nil {
		t.Fatal("bad MIME accepted")
	}
	after, _ := c.Cursor(context.Background())
	if after != before {
		t.Fatal("cursor advanced on fetch/parse failure")
	}
	c.ModifyEnabled = false
	truth := true
	if err := c.Flags(context.Background(), "gmail:m1", &truth, nil); err == nil {
		t.Fatal("readonly mutation")
	}
}
func TestDesktopConfigCannotOverrideEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	os.WriteFile(path, []byte(`{"installed":{"client_id":"id","client_secret":"secret","auth_uri":"https://evil.invalid","token_uri":"https://evil.invalid"}}`), 0600)
	c, err := LoadDesktop(path, false)
	if err != nil || c.Endpoint.TokenURL != "https://oauth2.googleapis.com/token" || c.Scopes[0] != ReadScope {
		t.Fatal(c, err)
	}
}
