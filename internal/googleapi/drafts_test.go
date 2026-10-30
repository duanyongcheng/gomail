package googleapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDraftWriteSingleAttempt(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		for _, tc := range []struct {
			status  int
			body    string
			unknown bool
			success bool
		}{
			{200, `{"id":"draft1","message":{"id":"msg2"}}`, false, true},
			{200, `{}`, true, false}, {200, `{bad`, true, false}, {503, `{}`, true, false}, {408, `{}`, true, false},
			{429, `{}`, false, false}, {403, `{}`, false, false}, {404, `{}`, false, false}, {307, `{}`, true, false},
		} {
			t.Run(fmt.Sprint(method, tc.status, tc.body), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != method {
						t.Error(r.Method)
					}
					if r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("token missing")
					}
					w.Header().Set("Location", "/redirect")
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				}))
				defer server.Close()
				api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
				id := ""
				if method == "PUT" {
					id = "draft1"
				}
				result, err := api.WriteDraft(t.Context(), id, []byte("mime"), "")
				if calls.Load() != 1 || result.Unknown != tc.unknown || (err == nil) != tc.success {
					t.Fatal(calls.Load(), result, err)
				}
			})
		}
	}
}
func TestDraftIDsAndLostResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
	for _, id := range []string{"../messages/x", "x/y", "x?query", "x\r\n", "x%2fy"} {
		if _, err := api.Draft(t.Context(), id); err == nil {
			t.Fatal("invalid id accepted")
		}
		if _, err := api.WriteDraft(t.Context(), id, []byte("mime"), ""); err == nil {
			t.Fatal("invalid write id accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid IDs reached network")
	}
	result, err := api.WriteDraft(t.Context(), "", []byte("mime"), "")
	if err == nil || !result.Unknown || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
}
