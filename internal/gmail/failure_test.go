package gmail

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestHistoryPagesReadRetriesAndFailureKeepsCursor(t *testing.T) {
	c, m := setup(t)
	m.Set("m1", raw(t, "first"), []string{"INBOX"})
	m.Set("m2", raw(t, "second"), []string{"INBOX"})
	if _, err := c.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.HistoryPages = true
	m.FailStatus = 429
	m.mu.Unlock()
	if _, err := c.Sync(context.Background()); err != nil {
		t.Fatal("429 bounded retry/history pages", err)
	}
	m.mu.Lock()
	m.FailStatus = 401
	m.mu.Unlock()
	if _, err := c.Sync(context.Background()); err != nil {
		t.Fatal("401 refresh retry", err)
	}
	m.mu.Lock()
	n := m.Refreshes
	m.mu.Unlock()
	if n != 1 {
		t.Fatal("401 not refreshed", n)
	}
	before, _ := c.Cursor(context.Background())
	m.ChangeHistory("900719925474099312349")
	m.mu.Lock()
	m.FailMessage = "m2"
	m.mu.Unlock()
	if _, err := c.Sync(context.Background()); err == nil {
		t.Fatal("persistent fetch failure ignored")
	}
	after, _ := c.Cursor(context.Background())
	if after != before {
		t.Fatal("cursor advanced", before, after)
	}
	m.mu.Lock()
	m.FailMessage = ""
	m.PageLoop = true
	m.mu.Unlock()
	if _, err := c.Sync(context.Background()); err == nil {
		t.Fatal("history loop ignored")
	}
}
func TestRefreshFailureKeepsEncryptedToken(t *testing.T) {
	m := NewMock()
	defer m.Close()
	tokens := testToken(t, m)
	if err := tokens.Install(&oauth2.Token{AccessToken: "old", RefreshToken: "demo-refresh", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	v := tokens.vault.(*Vault)
	before, _ := os.ReadFile(v.Path)
	m.mu.Lock()
	m.RejectRefresh = true
	m.mu.Unlock()
	if _, err := tokens.Token(context.Background()); err == nil {
		t.Fatal("invalid grant ignored")
	}
	after, _ := os.ReadFile(v.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("refresh failure overwrote token")
	}
}
func TestOAuthDenialDuplicateStateAndPKCE(t *testing.T) {
	m := NewMock()
	defer m.Close()
	_, err := Authorize(context.Background(), m.OAuthConfig(), m.Server.Client(), func(authURL string) error {
		u, _ := url.Parse(authURL)
		redirect, _ := url.Parse(u.Query().Get("redirect_uri"))
		state := u.Query().Get("state")
		q := url.Values{"state": {state, state}, "code": {"bad"}}
		redirect.RawQuery = q.Encode()
		resp, e := http.Get(redirect.String())
		if e != nil {
			return e
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatal("duplicate state accepted")
		}
		q = url.Values{"state": {state}, "error": {"access_denied"}}
		redirect.RawQuery = q.Encode()
		resp, e = http.Get(redirect.String())
		if e == nil {
			resp.Body.Close()
		}
		return e
	})
	if err == nil {
		t.Fatal("denial ignored")
	}
}
