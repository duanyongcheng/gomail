package gmail_test

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/store"
)

type blockingRoundTripper struct {
	base    http.RoundTripper
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (b *blockingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/messages/m1") {
		b.once.Do(func() { close(b.started) })
		select {
		case <-b.release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	return b.base.RoundTrip(r)
}

func TestGmailSyncAndReplayKeepLocalIntentUntilRemoteConfirmation(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	block := &blockingRoundTripper{base: mock.Server.Client().Transport, started: make(chan struct{}), release: make(chan struct{})}
	p.API.HTTP = &http.Client{Transport: block}
	done := make(chan error, 1)
	go func() { _, err := p.Sync(t.Context()); done <- err }()
	select {
	case <-block.started:
	case <-time.After(3 * time.Second):
		close(block.release)
		<-done
		t.Fatal("sync never fetched")
	}
	// Enqueue while a pass already owns the provider gate and has started I/O.
	_, queueErr := db.QueueMailMutation(t.Context(), store.MailMutation{MessageIDs: []string{"a:m1"}, OpType: "archive"})
	close(block.release)
	err = <-done
	if queueErr != nil || err != nil {
		t.Fatal(queueErr, err)
	}
	assertInbox := func(want bool) {
		t.Helper()
		got, err := db.MessageInFolderRemote(t.Context(), "a:m1", "INBOX")
		if err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	assertInbox(false)
	ops, payloads, err := db.ClaimPendingOps(t.Context(), 1, time.Now().Unix())
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	if _, err := p.Replay(t.Context(), ops[0].OpType, payloads[ops[0].ID]); err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePendingOp(t.Context(), ops[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertInbox(false)
	// After confirmation, another client's update must not be permanently masked.
	mock.Lock()
	message := mock.Messages["m1"]
	message.Labels = append(message.Labels, "INBOX")
	mock.Messages["m1"] = message
	mock.Unlock()
	if _, err := p.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertInbox(true)
	for _, kind := range []string{"delete", "restore"} {
		if _, err := db.QueueMailMutation(t.Context(), store.MailMutation{MessageIDs: []string{"a:m1"}, OpType: kind}); err != nil {
			t.Fatal(err)
		}
		ops, payloads, err := db.ClaimPendingOps(t.Context(), 1, time.Now().Unix())
		if err != nil || len(ops) != 1 {
			t.Fatal(ops, err)
		}
		if _, err := p.Replay(t.Context(), ops[0].OpType, payloads[ops[0].ID]); err != nil {
			t.Fatal(err)
		}
		if err := db.CompletePendingOp(t.Context(), ops[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	remote, err := p.API.Message(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range remote.Labels {
		if label == "TRASH" || label == "SPAM" {
			t.Fatal("restore retained trash/spam", remote.Labels)
		}
	}
	assertInbox(true)
}
