package googleapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestListDraftsAndMetadataStayBounded(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer secret" || r.Method != http.MethodGet {
			t.Error("wrong request")
		}
		if r.URL.Path == "/drafts" {
			if r.URL.Query().Get("maxResults") != "20" || r.URL.Query().Get("pageToken") != "opaque+page" {
				t.Error(r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"drafts":[{"id":"d1","message":{"id":"m1","threadId":"t1"}}],"nextPageToken":"next"}`)
		} else {
			if r.URL.Path != "/drafts/d1" || r.URL.Query().Get("format") != "metadata" {
				t.Error(r.URL)
			}
			fmt.Fprint(w, `{"id":"d1","message":{"id":"m1","internalDate":"123000","payload":{"headers":[{"name":"Subject","value":"subject"}]}}}`)
		}
	}))
	defer server.Close()
	api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
	for _, token := range []string{strings.Repeat("x", 4097), "page\n", "\xff"} {
		if _, err := api.ListDrafts(t.Context(), token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid token reached network")
	}
	page, err := api.ListDrafts(t.Context(), "opaque+page")
	if err != nil || len(page.Drafts) != 1 || page.Next != "next" {
		t.Fatal(page, err)
	}
	metadata, err := api.DraftMetadata(t.Context(), "d1")
	if err != nil || metadata.Message.ID != "m1" || metadata.Message.Date != "123000" || len(metadata.Message.Payload.Headers) != 1 || metadata.Message.Payload.Headers[0].Value != "subject" {
		t.Fatal(metadata, err)
	}
}

func TestListDraftsRejectsMalformedAndLoopingPages(t *testing.T) {
	for _, body := range []string{
		`{"drafts":[{"id":"d","message":{"id":"m"}},{"id":"d","message":{"id":"n"}}]}`,
		`{"drafts":[{"id":"../d","message":{"id":"m"}}]}`,
		`{"drafts":[{"id":"d","message":{}}]}`,
		`{"drafts":[],"nextPageToken":"current"}`,
		`{"drafts":[],"nextPageToken":"bad\npage"}`,
		`{"drafts":[` + strings.TrimSuffix(strings.Repeat(`{"id":"d","message":{"id":"m"}},`, 21), ",") + `]}`,
	} {
		t.Run(body[:min(len(body), 70)], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
			if _, err := api.ListDrafts(t.Context(), "current"); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
}

func TestDeleteDraftSingleAttemptAndConservativeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		unknown bool
		success bool
	}{
		{200, `{}`, false, true}, {204, "", false, true},
		{200, `{bad`, true, false}, {200, `null`, true, false}, {200, `{"id":"unrelated"}`, true, false},
		{503, `{}`, true, false}, {408, `{}`, true, false}, {307, `{}`, true, false},
		{429, `{}`, false, false}, {403, `{}`, false, false}, {404, `{}`, false, false},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodDelete || r.URL.Path != "/drafts/draft1" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("wrong delete request")
				}
				w.Header().Set("Location", "/followed")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
			result, err := api.DeleteDraft(t.Context(), "draft1")
			if calls.Load() != 1 || result.Unknown != tc.unknown || (err == nil) != tc.success {
				t.Fatal(calls.Load(), result, err)
			}
		})
	}
}

func TestDeleteDraftLostResponseAndInvalidRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}))
	defer server.Close()
	api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
	for _, id := range []string{"", "../d", "d/x", "d\n", strings.Repeat("d", 513)} {
		if _, err := api.DeleteDraft(t.Context(), id); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := api.DeleteDraft(ctx, "draft"); err == nil || result.Unknown {
		t.Fatal("pre-submit cancellation was unknown", result, err)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid/cancelled request reached network")
	}
	result, err := api.DeleteDraft(t.Context(), "draft")
	if err == nil || !result.Unknown || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
}

func TestSendDraftImmutablePayloadAndSingleAttempt(t *testing.T) {
	for _, tc := range []struct {
		status         int
		body           string
		unknown, retry bool
		remote         string
	}{
		{200, `{"id":"sent-1"}`, false, false, "sent-1"},
		{200, `{bad`, true, false, ""}, {200, `{}`, true, false, ""},
		{500, `{}`, true, false, ""}, {408, `{}`, true, false, ""},
		{429, `{}`, false, true, ""}, {403, `{}`, false, false, ""},
		{404, `{}`, false, false, ""}, {307, `{}`, true, false, ""},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/drafts/send" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("wrong draft send request")
				}
				var input Draft
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.ID != "stable-draft" || input.Message.ThreadID != "reply-thread" || input.Message.Raw != base64.RawURLEncoding.EncodeToString([]byte("immutable MIME")) {
					t.Error("immutable draft payload was not sent", input)
				}
				w.Header().Set("Location", "/messages/send")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
			got := api.SendDraftRaw(t.Context(), "stable-draft", []byte("immutable MIME"), "reply-thread")
			if calls.Load() != 1 || got.Unknown != tc.unknown || got.Retryable != tc.retry || got.RemoteID != tc.remote {
				t.Fatal(calls.Load(), got)
			}
		})
	}
}

func TestDraftSessionPinsReadsDeleteAndSend(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Error("session token changed")
		}
		switch r.URL.Path {
		case "/profile":
			fmt.Fprint(w, `{"emailAddress":"sender@example.invalid","historyId":"1"}`)
		case "/drafts/send":
			fmt.Fprint(w, `{"id":"sent"}`)
		default:
			fmt.Fprint(w, `{}`)
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
	if _, err := session.DeleteDraft(t.Context(), "draft"); err != nil {
		t.Fatal(err)
	}
	if result := session.SendDraftRaw(t.Context(), "another-draft", []byte("MIME"), ""); result.RemoteID != "sent" {
		t.Fatal(result)
	}
	if tokens.calls.Load() != 1 || calls.Load() != 3 {
		t.Fatal(tokens.calls.Load(), calls.Load())
	}
}

func TestSendDraftInvalidAndCancelledNeverSubmits(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	api := &API{Base: server.URL, HTTP: server.Client(), Tokens: fixedToken("secret")}
	for _, id := range []string{"", "../d", "d/x", "d\n", strings.Repeat("d", 513)} {
		if result := api.SendDraftRaw(t.Context(), id, []byte("MIME"), ""); result.RemoteID != "" || result.Unknown || result.Retryable {
			t.Fatal("invalid ID accepted", result)
		}
	}
	if result := api.SendDraftRaw(t.Context(), "draft", nil, ""); result.RemoteID != "" || result.Unknown || result.Retryable {
		t.Fatal("empty draft accepted", result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result := api.SendDraftRaw(ctx, "draft", []byte("MIME"), ""); result.Unknown || !result.Retryable {
		t.Fatal("pre-submit cancellation was unknown", result)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid/cancelled send reached network")
	}
}
