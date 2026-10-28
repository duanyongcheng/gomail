package googleapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

type changingSendToken struct{ calls atomic.Int32 }

func (t *changingSendToken) Token(context.Context) (string, error) {
	return fmt.Sprint("token-", t.calls.Add(1)), nil
}
func TestSendingSessionPinsTokenAcrossIdentityCheckAndPOST(t *testing.T) {
	var mu sync.Mutex
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path == "/profile" {
			fmt.Fprint(w, `{"emailAddress":"sender@example.invalid","historyId":"1"}`)
		} else {
			fmt.Fprint(w, `{"id":"sent"}`)
		}
	}))
	defer server.Close()
	tokens := &changingSendToken{}
	api := &API{Base: server.URL, HTTP: server.Client(), Tokens: tokens}
	session, err := api.SendingSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Profile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if result := session.SendRaw(t.Context(), []byte("message")); result.RemoteID != "sent" {
		t.Fatal(result)
	}
	mu.Lock()
	defer mu.Unlock()
	if tokens.calls.Load() != 1 || len(authorizations) != 2 || authorizations[0] != authorizations[1] {
		t.Fatal(tokens.calls.Load(), authorizations)
	}
}
