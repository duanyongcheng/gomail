package gmail_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/provider/gmail"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"github.com/duanyongcheng/pebble-go/internal/testgmail"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMalformedMailDoesNotBlockScanAndRecoversAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	p, mock, source := fixture(t, db, "a")
	mock.Lock()
	valid := mock.Messages["m1"]
	mock.Unlock()
	mock.Set("m1", []byte("malformed private payload"), []string{"INBOX", "UNREAD"})
	result, err := p.Sync(t.Context())
	if err != nil || result.Limited || result.DeferredCount != 1 || countCached(t, db) != 1 {
		t.Fatal("bad mail blocked scan", result, err)
	}
	state, _ := db.GmailState(t.Context(), "a")
	if state.Checkpoint.Phase != "complete" || state.Cursor == "" {
		t.Fatal("failure was not durably skipped", state)
	}
	page, err := db.DownloadFailures(t.Context(), "a", 20, 0)
	if err != nil || page.Total != 1 || page.Items[0].Kind != "invalid_content" || page.Items[0].Attempts != 1 {
		t.Fatal("missing safe failure record", page, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p = gmail.New(db, mock.API(source), "a")
	mock.Lock()
	mock.Messages["m1"] = valid
	mock.Requests = nil
	mock.Unlock()
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	requests := append([]string{}, mock.Requests...)
	mock.Unlock()
	for _, request := range requests {
		if strings.HasSuffix(request, "/messages/m1") {
			t.Fatal("failed mail was retried before backoff")
		}
	}
	if err := db.QueueMessageFailures(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	result, err = p.Sync(t.Context())
	if err != nil || result.DeferredCount != 0 || countCached(t, db) != 2 || len(result.NewMessages) != 0 {
		t.Fatal("historical mail did not recover silently", result, err)
	}
}

func TestIncrementalRecoveryAnnouncesOnlyOnceAndPreservesCachedMail(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, _ := db.Message(t.Context(), "a:m1")
	attachments, _ := db.Attachments(t.Context(), "a:m1")
	mock.Lock()
	valid := mock.Messages["m1"]
	mock.Unlock()
	mock.Set("m1", []byte("malformed"), []string{"INBOX", "UNREAD"})
	mock.Set("new", []byte("malformed"), []string{"INBOX", "UNREAD"})
	mock.Lock()
	mock.HistoryRefs = []testgmail.Ref{{ID: "m1"}, {ID: "new"}}
	mock.HistoryID = "900719925474099399999"
	mock.Unlock()
	result, err := p.Sync(t.Context())
	if err != nil || result.DeferredCount != 2 || len(result.NewMessages) != 0 {
		t.Fatal("incremental failure was lost", result, err)
	}
	after, _ := db.Message(t.Context(), "a:m1")
	cachedAttachments, _ := db.Attachments(t.Context(), "a:m1")
	if before.BodyText != after.BodyText || len(cachedAttachments) != len(attachments) || *cachedAttachments[0].LocalPath != *attachments[0].LocalPath {
		t.Fatal("content failure damaged cached body or attachments")
	}
	mock.Lock()
	mock.Messages["m1"] = valid
	newMessage := valid
	newMessage.ID = "new"
	mock.Messages["new"] = newMessage
	mock.Unlock()
	if err := db.QueueMessageFailures(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	result, err = p.Sync(t.Context())
	if err != nil || result.DeferredCount != 0 || len(result.NewMessages) != 1 || result.NewMessages[0].MessageID != "a:new" {
		t.Fatal("recovery announcement missing or duplicated", result, err)
	}
	result, err = p.Sync(t.Context())
	if err != nil || len(result.NewMessages) != 0 {
		t.Fatal("replay announced twice", result, err)
	}
}

func TestLargeAttachmentAndBoundedPrefixSurviveTransientFailure(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	content := strings.Repeat("x", 9<<20)
	raw := []byte("From: sender@example.invalid\r\nSubject: large attachment\r\nContent-Type: multipart/mixed; boundary=p\r\n\r\n--p\r\nContent-Type: text/plain\r\n\r\nsearchable body\r\n--p\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=large.bin\r\nContent-Transfer-Encoding: binary\r\n\r\n" + content + "\r\n--p--\r\n")
	mock.Set("m1", raw, []string{"INBOX", "UNREAD"})
	mock.Lock()
	mock.PageSize = 100
	mock.FailMessage = "m2"
	mock.Unlock()
	if _, err := p.Sync(t.Context()); err == nil {
		t.Fatal("transient failure was skipped")
	}
	if countCached(t, db) != 1 {
		t.Fatal("bounded prefix was not committed")
	}
	state, _ := db.GmailState(t.Context(), "a")
	if state.Cursor != "" || state.Checkpoint.PageToken != "" {
		t.Fatal("incomplete page advanced checkpoint", state)
	}
	page, _ := db.DownloadFailures(t.Context(), "a", 20, 0)
	if page.Total != 0 {
		t.Fatal("transient failure was quarantined")
	}
	mock.Lock()
	mock.FailMessage = ""
	mock.Unlock()
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	attachments, err := db.Attachments(t.Context(), "a:m1")
	if err != nil || len(attachments) != 1 || attachments[0].Size != int64(len(content)) {
		t.Fatal("large attachment metadata", attachments, err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(db.Path()), *attachments[0].LocalPath))
	if err != nil || string(data) != content {
		t.Fatal("large attachment not persisted", err)
	}
}

func TestMessageErrorsKeepTransientFailuresOutOfRecoveryQueue(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			_, mock, source := fixture(t, db, "a")
			api := mock.API(source)
			api.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/messages/m1") {
					return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"180"}}, Body: io.NopCloser(strings.NewReader("private remote error"))}, nil
				}
				return http.DefaultTransport.RoundTrip(r)
			})}
			p := gmail.New(db, api, "a")
			if _, err := p.Sync(t.Context()); err == nil {
				t.Fatal("transient HTTP error was skipped")
			}
			page, _ := db.DownloadFailures(t.Context(), "a", 20, 0)
			cursor, _ := db.Cursor(t.Context(), "a")
			if page.Total != 0 || cursor != "" {
				t.Fatal("transient failure advanced cursor or entered queue", page, cursor)
			}
		})
	}
}

func TestOversizedResponseIsRecordedWhileOtherMailContinues(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, mock, source := fixture(t, db, "a")
	api := mock.API(source)
	api.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/messages/m1") {
			return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 1 << 30, Body: io.NopCloser(strings.NewReader("not read"))}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	result, err := gmail.New(db, api, "a").Sync(t.Context())
	page, _ := db.DownloadFailures(t.Context(), "a", 20, 0)
	if err != nil || result.DeferredCount != 1 || page.Total != 1 || page.Items[0].Kind != "too_large" || countCached(t, db) != 1 {
		t.Fatal("oversized message blocked mailbox", result, page, err)
	}
}

func TestRecoveryRetryBudgetAndConfirmedDeletion(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	refs := addMessages(t, mock, 25)
	mock.Lock()
	valid := mock.Messages[refs[0].ID]
	for _, ref := range refs {
		m := mock.Messages[ref.ID]
		m.Raw = base64.RawURLEncoding.EncodeToString([]byte("malformed"))
		mock.Messages[ref.ID] = m
	}
	mock.PageSize = 100
	mock.HistoryRefs = []testgmail.Ref{}
	mock.Unlock()
	result, err := p.Sync(t.Context())
	if err != nil || result.DeferredCount != 25 || result.Limited {
		t.Fatal("initial recovery records", result, err)
	}
	mock.Lock()
	for _, ref := range refs {
		m := valid
		m.ID = ref.ID
		mock.Messages[ref.ID] = m
	}
	mock.Unlock()
	mock.Delete(refs[0].ID)
	if err := db.QueueMessageFailures(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{15, 5, 0} {
		result, err = p.Sync(t.Context())
		if err != nil || result.DeferredCount != want || result.Limited != (want > 0) {
			t.Fatal("retry budget or continuation", want, result, err)
		}
	}
	if countCached(t, db) != 26 {
		t.Fatal("recovery lost mail or retained confirmed missing mail", countCached(t, db))
	}
}

func TestCancelledMessageRequestDoesNotEnterRecoveryQueue(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, mock, source := fixture(t, db, "a")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	api := mock.API(source)
	api.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/messages/m1") {
			cancel()
			return nil, ctx.Err()
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	if _, err := gmail.New(db, api, "a").Sync(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel error was lost", err)
	}
	page, _ := db.DownloadFailures(t.Context(), "a", 20, 0)
	if page.Total != 0 {
		t.Fatal("cancellation recorded as bad content")
	}
}
