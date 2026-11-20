package gmail_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/store"
	"github.com/bary/pebble-go/internal/testgmail"
)

func addMessages(t *testing.T, mock *testgmail.Mock, count int) []testgmail.Ref {
	t.Helper()
	refs := make([]testgmail.Ref, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("bulk%04d", i)
		raw := []byte("From: sender@example.invalid\r\nTo: to@example.invalid\r\nSubject: backfill searchable\r\nMessage-ID: <" + id + "@example.invalid>\r\n\r\nsearchable body")
		mock.Set(id, raw, []string{"INBOX", "UNREAD"})
		refs = append(refs, testgmail.Ref{ID: id})
	}
	return refs
}

func countCached(t *testing.T, db *store.DB) int {
	t.Helper()
	ids, err := db.KnownRemoteIDs(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	return len(ids)
}

func TestRateLimitedBackfillResumesSamePageAndCompletes(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, mock, source := fixture(t, db, "a")
	mock.Delete("m1")
	mock.Delete("m2")
	addMessages(t, mock, 250)
	mock.PageSize = 100
	api := mock.API(source)
	calls := 0
	api.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/messages/bulk0100") {
			calls++
			if calls <= 2 {
				state, err := db.GmailState(t.Context(), "a")
				if err != nil || state.Checkpoint.PageToken != "100" || state.Cursor != "" || countCached(t, db) != 100 {
					t.Fatal("rate limit advanced the incomplete page", state, err)
				}
			}
			if calls == 1 {
				return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"errors":[{"reason":"rateLimitExceeded"}]}}`))}, nil
			}
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	p := gmail.New(db, api, "a")
	result, err := p.Sync(t.Context())
	state, stateErr := db.GmailState(t.Context(), "a")
	if err != nil || stateErr != nil || result.Limited || result.DeferredCount != 0 || state.Checkpoint.Phase != "complete" || state.Cursor == "" || countCached(t, db) != 250 || calls != 2 {
		t.Fatal("rate-limited backfill did not complete without another manual trigger", result, state, calls, err, stateErr)
	}
	if _, err := p.Sync(t.Context()); err != nil || countCached(t, db) != 250 {
		t.Fatal("incremental replay duplicated or lost messages", err)
	}
}

func TestFullBackfillResumesFailedPageAfterDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	p, mock, source := fixture(t, db, "a")
	mock.Delete("m1")
	mock.Delete("m2")
	addMessages(t, mock, 250)
	mock.PageSize = 100
	mock.Lock()
	mock.FailMessage = "bulk0100"
	mock.Unlock()
	if _, err := p.Sync(t.Context()); err == nil {
		t.Fatal("transient page failure accepted")
	}
	state, err := db.GmailState(t.Context(), "a")
	if err != nil || state.Cursor != "" || state.Checkpoint.PageToken != "100" || countCached(t, db) != 100 {
		t.Fatal("failed page advanced recovery boundary", state, err)
	}
	mock.Lock()
	mock.FailMessage = ""
	mock.RequestURIs = nil
	mock.Unlock()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p = gmail.New(db, mock.API(source), "a")
	result, err := p.Sync(t.Context())
	if err != nil || result.Limited || countCached(t, db) != 250 || len(result.NewMessages) != 0 {
		t.Fatal("resume did not complete full mailbox", result, err)
	}
	mock.Lock()
	uris := append([]string{}, mock.RequestURIs...)
	mock.Unlock()
	for _, uri := range uris {
		if strings.Contains(uri, "/messages?") {
			u, _ := url.Parse(uri)
			if u.Query().Get("pageToken") != "100" {
				t.Fatal("resume restarted committed page", uri)
			}
			break
		}
	}
	hits, err := db.Search(t.Context(), "searchable", "a", 100)
	if err != nil || len(hits) != 100 {
		t.Fatal("backfilled mail missing from FTS", len(hits), err)
	}
}

func TestBackfillPassIsBoundedAndCancellationKeepsCheckpoint(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	mock.Delete("m1")
	mock.Delete("m2")
	addMessages(t, mock, 25)
	result, err := p.Sync(t.Context())
	if err != nil || !result.Limited || result.Count != 10 {
		t.Fatal("pass did not bound work", result, err)
	}
	before, _ := db.GmailState(t.Context(), "a")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Sync(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	after, _ := db.GmailState(t.Context(), "a")
	if before.Checkpoint.PageToken != after.Checkpoint.PageToken || countCached(t, db) != 10 {
		t.Fatal("cancellation lost checkpoint")
	}
	for i := 0; i < 3; i++ {
		result, err = p.Sync(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !result.Limited {
			break
		}
	}
	if result.Limited || countCached(t, db) != 25 {
		t.Fatal("subsequent passes did not finish", result)
	}
}

func TestExpiredHistoryReconcilesOnlyConfirmedDeletions(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	mock.Delete("m1")
	mock.Lock()
	mock.Expired = true
	mock.OmitFromList = map[string]bool{"m2": true}
	mock.Unlock()
	addMessages(t, mock, 1)
	result, err := p.Sync(t.Context())
	if err != nil || result.Limited || !result.Initial {
		t.Fatal("expired history did not recover", result, err)
	}
	if _, err := db.Message(t.Context(), "a:m2"); err != nil {
		t.Fatal("list omission deleted existing mail", err)
	}
	if _, err := db.Message(t.Context(), "a:m1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("confirmed deletion retained", err)
	}
	if countCached(t, db) != 2 {
		t.Fatal("reconciliation lost mail")
	}
	pending, err := db.BackfillPending(t.Context(), "a", 100)
	if err != nil || len(pending) != 0 {
		t.Fatal("reconciliation leaked pending entries", pending, err)
	}
}

func TestHistoryPagesResumeAndAnnounceOnlyCommittedNewMail(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, source := fixture(t, db, "a")
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, _ := db.Cursor(t.Context(), "a")
	refs := addMessages(t, mock, 250)
	mock.Lock()
	mock.HistoryRefs = refs
	mock.FailMessage = "bulk0100"
	mock.HistoryID = "900719925474099399999"
	mock.Unlock()
	result, err := p.Sync(t.Context())
	if err == nil || len(result.NewMessages) != 100 || countCached(t, db) != 102 {
		t.Fatal("history page recovery failed", len(result.NewMessages), err)
	}
	state, err := db.GmailState(t.Context(), "a")
	if err != nil || state.Cursor != before || state.Checkpoint.PageToken != "100" {
		t.Fatal("failed history page advanced cursor", state, err)
	}
	mock.Lock()
	mock.FailMessage = ""
	mock.Unlock()
	p = gmail.New(db, mock.API(source), "a")
	result, err = p.Sync(t.Context())
	if err != nil || result.Limited || len(result.NewMessages) != 150 || countCached(t, db) != 252 {
		t.Fatal("history did not resume", len(result.NewMessages), err)
	}
	result, err = p.Sync(t.Context())
	if err != nil || len(result.NewMessages) != 0 {
		t.Fatal("replayed history announced duplicate mail", err)
	}
}

func TestBackfillCatchesChangesFromOriginalBaseline(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	mock.Delete("m1")
	mock.Delete("m2")
	addMessages(t, mock, 250)
	mock.PageSize = 100
	baseline := "900719925474099312345"
	changed := false
	result, err := p.SyncWithProgress(t.Context(), func(event domain.SyncProgress) {
		if changed || event.Phase != "messages" {
			return
		}
		changed = true
		// This delivery sorts before the already committed first page, so
		// continuing the list will miss it. History must bridge the scan.
		raw := []byte("From: sender@example.invalid\r\nTo: to@example.invalid\r\nSubject: arrival during backfill\r\n\r\nnew body")
		mock.Set("aaa-arrival", raw, []string{"INBOX", "UNREAD"})
		mock.Lock()
		mock.HistoryID = "900719925474099399999"
		mock.HistoryRefs = []testgmail.Ref{{ID: "aaa-arrival"}}
		mock.Unlock()
	})
	if err != nil || result.Limited || countCached(t, db) != 251 || len(result.NewMessages) != 0 {
		t.Fatal("scan lost concurrent delivery", result, err)
	}
	mock.Lock()
	uris := append([]string{}, mock.RequestURIs...)
	mock.Unlock()
	found := false
	for _, uri := range uris {
		if strings.Contains(uri, "/history?") {
			u, _ := url.Parse(uri)
			if u.Query().Get("startHistoryId") != baseline {
				t.Fatal("scan skipped baseline changes", uri)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("scan did not catch up History")
	}
}

func TestExpiredMessageContinuationRestartsWithoutLosingCache(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, _, _ := fixture(t, db, "a")
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	cursor, _ := db.Cursor(t.Context(), "a")
	checkpoint := &store.GmailCheckpoint{Phase: "messages", HistoryID: cursor, PageToken: "expired", SuppressNotifications: true}
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Checkpoint: checkpoint, ResetBackfill: true}); err != nil {
		t.Fatal(err)
	}
	result, err := p.Sync(t.Context())
	if err != nil || !result.Limited || countCached(t, db) != 2 {
		t.Fatal("expired page destroyed cache", result, err)
	}
	state, err := db.GmailState(t.Context(), "a")
	if err != nil || state.Cursor != cursor || state.Checkpoint.PageToken != "" {
		t.Fatal("expired page did not restart safely", state, err)
	}
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestOldM0CursorStillRequiresFullBackfillAndLoopPreservesCache(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	cursor := "900719925474099300000"
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Cursor: &cursor}); err != nil {
		t.Fatal(err)
	}
	mock.PageLoop = true
	if _, err := p.Sync(t.Context()); err == nil {
		t.Fatal("pagination loop accepted")
	}
	after, _ := db.Cursor(t.Context(), "a")
	if after != cursor {
		t.Fatal("loop advanced history cursor")
	}
	mock.Lock()
	mock.PageLoop = false
	mock.Unlock()
	if _, err := p.SyncWithProgress(t.Context(), func(event domain.SyncProgress) {
		if event.AccountID != "a" {
			t.Error("cross-account progress")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if countCached(t, db) != 2 {
		t.Fatal("M0 cache was not fully backfilled")
	}
}

// A long backfill is otherwise silent: the profile log only shows committed
// pages, so someone diagnosing a slow or stalled sync has something to read.
func TestCommittedPagesAreLogged(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	mock.PageSize = 1
	var log bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := log.String()
	// One record per committed page, plus the reconcile and history phases.
	if strings.Count(got, `"phase":"messages"`) != 2 || !strings.Contains(got, `"phase":"history"`) {
		t.Fatal("committed pages were not logged", got)
	}
	if strings.Contains(got, "example.invalid") || strings.Contains(got, "中文") {
		t.Fatal("page logging leaked addresses or content", got)
	}
}
