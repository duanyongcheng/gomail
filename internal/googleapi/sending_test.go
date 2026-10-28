package googleapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSendRawSingleAttemptAndConservativeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		status         int
		body           string
		unknown, retry bool
		remote         string
	}{
		{200, `{"id":"sent-1"}`, false, false, "sent-1"},
		{200, `{bad`, true, false, ""}, {200, `{}`, true, false, ""},
		{500, `{}`, true, false, ""}, {408, `{}`, true, false, ""},
		{429, `{}`, false, true, ""}, {403, `{}`, false, false, ""}, {400, `{}`, false, false, ""},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
			got := api.SendRaw(t.Context(), []byte("MIME"))
			if calls.Load() != 1 || got.Unknown != tc.unknown || got.Retryable != tc.retry || got.RemoteID != tc.remote {
				t.Fatal(calls.Load(), got)
			}
		})
	}
}
func TestSendRawDoesNotFollowRedirectAndLostResponseIsUnknown(t *testing.T) {
	for _, kind := range []string{"redirect", "disconnect", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch kind {
				case "redirect":
					http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
				case "disconnect":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				case "oversized":
					fmt.Fprint(w, strings.Repeat("x", defaultResponseLimit+1))
				}
			}))
			defer server.Close()
			api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
			result := api.SendRaw(t.Context(), []byte("message"))
			if calls.Load() != 1 || !result.Unknown || result.Retryable {
				t.Fatal(calls.Load(), result)
			}
		})
	}
}
func TestSendCancelledBeforeSubmissionDoesNotCallServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("called server") }))
	defer server.Close()
	api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := api.SendRaw(ctx, []byte("message"))
	if result.Unknown || !result.Retryable {
		t.Fatal(result)
	}
}
