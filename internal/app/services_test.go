package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/oauth"
	"github.com/bary/pebble-go/internal/store"
	"golang.org/x/oauth2"
)

type testKeyring struct {
	key           []byte
	calls, writes int
	err           error
}

func (k *testKeyring) Get(string) ([]byte, error)     { k.calls++; return k.key, k.err }
func (k *testKeyring) Set(_ string, key []byte) error { k.writes++; k.key = key; return nil }

type rejectTransport struct{ calls int }

func (t *rejectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("startup attempted network access")
}

func TestStartupRestoresExpiredTokensWithoutNetworkOrBrowser(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	service, _ := crypto.FromKey(key)
	for _, id := range []string{"a", "b"} {
		encrypted, err := oauth.EncodeToken(service, id, "test-client", &oauth2.Token{AccessToken: "expired", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id, Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, encrypted); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	transport := &rejectTransport{}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = oldTransport }()
	browserCalls := 0
	ring := &testKeyring{key: key}
	runtime := New(t.Context(), db, ring, func(string) error { browserCalls++; return nil })
	if err := runtime.Restore(); err != nil {
		t.Fatal(err)
	}
	accounts, err := (&AccountService{Runtime: runtime}).List(t.Context(), Empty{})
	if err != nil || len(accounts) != 2 || !accounts[0].Capabilities.Read || !accounts[1].Capabilities.Read {
		t.Fatal("accounts were not restored", accounts, err)
	}
	if transport.calls != 0 || browserCalls != 0 || ring.writes != 0 {
		t.Fatal("startup had external side effects")
	}
	if _, err := (&AccountService{Runtime: runtime}).Delete(t.Context(), AccountRequest{AccountID: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.provider("a"); err == nil {
		t.Fatal("deleted account retained its provider")
	}
}

func TestEmptyStartupDoesNotAccessKeyring(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ring := &testKeyring{err: crypto.ErrKeyNotFound}
	runtime := New(context.Background(), db, ring, func(string) error { t.Fatal("browser opened"); return nil })
	if err := runtime.Restore(); err != nil {
		t.Fatal(err)
	}
	if ring.calls != 0 || ring.writes != 0 {
		t.Fatal("empty startup accessed keyring")
	}
	t.Setenv("GOOGLE_CLIENT_ID", "")
	service := &AccountService{Runtime: runtime}
	if service.Status(Empty{}).Configured {
		t.Fatal("missing configuration reported ready")
	}
	if _, err := service.Login(t.Context(), LoginRequest{Provider: "gmail"}); err == nil {
		t.Fatal("unconfigured login accepted")
	}
}
