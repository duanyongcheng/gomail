package gmail_test

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/googleapi"
	"github.com/duanyongcheng/gomail/internal/provider/gmail"
	"github.com/duanyongcheng/gomail/internal/store"
)

// A 200 body that stops mid-object is a cut stream, not undecodable mail.
// Quarantining it would park messages in the retry list that no retry can ever
// repair, and the pass would still claim it made progress.
func TestIncompleteMessageBodyFailsPassWithoutQuarantine(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, mock, source := fixture(t, db, "a")
	api := mock.API(source)
	valid := api.HTTP.Transport
	api.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/messages/m2") {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"m2","raw":"`))}, nil
		}
		return valid.RoundTrip(r)
	})}
	p := gmail.New(db, api, "a")

	_, syncErr := p.Sync(t.Context())
	if syncErr == nil {
		t.Fatal("an incomplete message body was accepted as a completed pass")
	}
	page, err := db.DownloadFailures(t.Context(), "a", 20, 0)
	if err != nil || page.Total != 0 {
		t.Fatal("truncated body was quarantined as undecodable content", page, err)
	}
	// The page's already-fetched prefix may commit, but the page must not be
	// consumed: the checkpoint stays on the same token and no cursor is set.
	state, err := db.GmailState(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if state.Cursor != "" || state.Checkpoint.Phase != "messages" {
		t.Fatal("failed pass claimed completion", state.Cursor, state.Checkpoint.Phase)
	}

	// The next pass resumes from the retained checkpoint and finishes the page.
	api.HTTP = &http.Client{Transport: valid}
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal("resumed pass failed", err)
	}
	if page, err := db.DownloadFailures(t.Context(), "a", 20, 0); err != nil || page.Total != 0 || countCached(t, db) != 2 {
		t.Fatal("resume quarantined mail or lost the page", page, err)
	}
	state, _ = db.GmailState(t.Context(), "a")
	if state.Checkpoint.Phase != "complete" || state.Cursor == "" {
		t.Fatal("backfill did not reach a complete checkpoint", state)
	}
}

// An account access block belongs to the account, not to the message being
// fetched, so it must fail the pass instead of quarantining that message.
func TestAccessBlockedDuringFetchIsNotQuarantined(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, mock, source := fixture(t, db, "a")
	api := mock.API(source)
	blocked := &domain.AccessError{State: domain.ConnectionReauthRequired}
	valid := api.HTTP.Transport
	api.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/messages/m2") {
			return nil, blocked
		}
		return valid.RoundTrip(r)
	})}
	p := gmail.New(db, api, "a")

	_, syncErr := p.Sync(t.Context())
	var access *domain.AccessError
	if !errors.As(syncErr, &access) {
		t.Fatal("blocked account did not fail the pass", syncErr)
	}
	page, err := db.DownloadFailures(t.Context(), "a", 20, 0)
	if err != nil || page.Total != 0 {
		t.Fatal("access block was recorded as a content failure", page, err)
	}
}

// The API layer must not present a cut 2xx body as undecodable content.
func TestTruncatedMessageBodyIsNotContentError(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, mock, source := fixture(t, db, "a")
	api := mock.API(source)
	valid := api.HTTP.Transport
	api.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/messages/m1") {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"m1","raw":`))}, nil
		}
		return valid.RoundTrip(r)
	})}
	_, err = api.Message(t.Context(), "m1")
	var content *googleapi.ContentError
	if errors.As(err, &content) {
		t.Fatal("cut stream was reported as undecodable message content", err)
	}
	if _, err := api.Message(t.Context(), "m2"); err != nil {
		t.Fatal("valid message failed after a cut stream", err)
	}
}

// Undecodable content still reaches the retry list, which is the only case the
// failure record is for.
func TestInvalidContentStillQuarantines(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	mock.Set("m2", []byte("not a mime message"), []string{"INBOX", "UNREAD"})

	result, err := p.Sync(t.Context())
	if err != nil || result.DeferredCount != 1 {
		t.Fatal("undecodable content was not quarantined", result, err)
	}
	page, err := db.DownloadFailures(t.Context(), "a", 20, 0)
	if err != nil || page.Total != 1 || page.Items[0].RemoteID != "m2" || page.Items[0].Kind != "invalid_content" {
		t.Fatal("missing safe failure record", page, err)
	}
	state, _ := db.GmailState(t.Context(), "a")
	if state.Checkpoint.Phase != "complete" {
		t.Fatal("quarantined content blocked the scan", state)
	}
}
