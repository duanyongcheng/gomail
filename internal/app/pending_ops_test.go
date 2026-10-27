package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/store"
)

type queuedWriter struct {
	mu      sync.Mutex
	calls   []string
	fail    error
	unknown bool
	block   chan struct{}
}

func (p *queuedWriter) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true, ModifyFlags: true}
}
func (p *queuedWriter) Sync(context.Context) (domain.SyncResult, error) {
	return domain.SyncResult{}, nil
}
func (p *queuedWriter) Flags(context.Context, string, *bool, *bool) error { return nil }
func (p *queuedWriter) Replay(ctx context.Context, opType string, payload json.RawMessage) (bool, error) {
	p.mu.Lock()
	p.calls = append(p.calls, opType+":"+string(payload))
	block, fail, unknown := p.block, p.fail, p.unknown
	p.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return unknown, fail
}

func pendingAppFixture(t *testing.T) (*Runtime, *queuedWriter, *store.DB) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Folders: []domain.Folder{{ID: "a:label:INBOX", AccountID: "a", RemoteID: "INBOX", Name: "Inbox", FolderType: "label"}}, Messages: []store.CachedMessage{{Message: domain.Message{ID: "a:m", AccountID: "a", RemoteID: "remote-m", Subject: "subject", BodyText: "body", To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}, CreatedAt: 1, UpdatedAt: 1}, FolderIDs: []string{"a:label:INBOX"}}}}); err != nil {
		t.Fatal(err)
	}
	r := New(t.Context(), db, nil, nil)
	t.Cleanup(r.Scheduler.Close)
	w := &queuedWriter{}
	r.InstallProvider("a", w)
	return r, w, db
}

func TestQueuedFlagsAreLocalImmediatelyAndReplayedInBackground(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	service := &MessageService{Runtime: r}
	read := true
	if _, err := service.QueueFlags(t.Context(), FlagsRequest{MessageID: "a:m", IsRead: &read}); err != nil {
		t.Fatal(err)
	}
	message, err := db.Message(t.Context(), "a:m")
	if err != nil || !message.IsRead {
		t.Fatal("local flag not applied", message, err)
	}
	summary, _ := db.PendingMailOpsSummary(t.Context(), "a")
	if summary.PendingCount != 1 {
		t.Fatal(summary)
	}
	r.processPendingOps(t.Context())
	writer.mu.Lock()
	calls := append([]string{}, writer.calls...)
	writer.mu.Unlock()
	if len(calls) != 1 {
		t.Fatal("replay missing", calls)
	}
	summary, _ = db.PendingMailOpsSummary(t.Context(), "a")
	if summary.TotalActiveCount != 0 {
		t.Fatal("completed op still active", summary)
	}
}

func TestQueuedWritesRetryWithBackoffAndNeverAutoRetryUnknownOutcome(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	writer.fail = errors.New("offline")
	if err := (&MessageService{Runtime: r}).queueFolderChange(t.Context(), "a:m", "archive"); err != nil {
		t.Fatal(err)
	}
	r.processPendingOps(t.Context())
	ops, _ := db.PendingMailOps(t.Context(), "a", 10)
	if len(ops) != 1 || ops[0].Status != "failed" || ops[0].Attempts != 1 || ops[0].NextRetryAt == nil {
		t.Fatal(ops)
	}
	writer.mu.Lock()
	writer.fail = nil
	writer.unknown = true
	writer.mu.Unlock()
	if _, err := db.RetryFailedPendingOps(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	r.processPendingOps(t.Context())
	ops, _ = db.PendingMailOps(t.Context(), "a", 10)
	if len(ops) != 1 || ops[0].Status != "failed" || ops[0].NextRetryAt != nil || ops[0].Result != "outcome_unknown" {
		t.Fatal("unknown outcome remained retryable", ops)
	}
}

func TestQueuedWriteWakesWorkerWithoutWaitingForPoll(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { r.RunPendingOpsWorker(ctx); close(done) }()
	if err := (&MessageService{Runtime: r}).queueFolderChange(t.Context(), "a:m", "archive"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		summary, _ := db.PendingMailOpsSummary(t.Context(), "a")
		writer.mu.Lock()
		calls := len(writer.calls)
		writer.mu.Unlock()
		if summary.TotalActiveCount == 0 && calls == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker waited for the polling interval")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestPendingOpsWorkerStopsOnCancellation(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	writer.block = make(chan struct{})
	if err := (&MessageService{Runtime: r}).queueFolderChange(t.Context(), "a:m", "archive"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.RunPendingOpsWorker(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		ops, _ := db.PendingMailOps(t.Context(), "a", 10)
		if len(ops) == 1 && ops[0].Status == "in_progress" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not claim op")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker ignored cancellation")
	}
}

func TestBatchAndAllReadQueueLocalChangesWithAccountScope(t *testing.T) {
	r, _, db := pendingAppFixture(t)
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: domain.Message{ID: "a:n", AccountID: "a", RemoteID: "remote-n", Subject: "n", To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}, CreatedAt: 1, UpdatedAt: 1}, FolderIDs: []string{"a:label:INBOX"}}}}); err != nil {
		t.Fatal(err)
	}
	service := &MessageService{Runtime: r}
	count, err := service.MarkFolderAllRead(t.Context(), AllReadRequest{AccountID: "a", FolderIDs: []string{"a:label:INBOX"}})
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	unread, _ := db.UnreadMessageIDs(t.Context(), "a", nil)
	if len(unread) != 0 {
		t.Fatal("local unread not cleared", unread)
	}
	summary, _ := db.PendingMailOpsSummary(t.Context(), "a")
	if summary.PendingCount != 2 {
		t.Fatal(summary)
	}
	starred := true
	if n, err := service.BatchFlags(t.Context(), BatchRequest{MessageIDs: []string{"a:m"}, Starred: &starred}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	message, _ := db.Message(t.Context(), "a:m")
	if !message.IsStarred {
		t.Fatal("batch star not local")
	}
	if _, err := service.MarkFolderAllRead(t.Context(), AllReadRequest{AccountID: "b", FolderIDs: []string{"a:label:INBOX"}}); err == nil {
		t.Fatal("cross-account folder accepted")
	}
	if _, err := service.DeletePermanent(t.Context(), ArchiveRequest{MessageID: "a:m"}); err == nil {
		t.Fatal("permanent delete accepted")
	}
}

func TestArchiveTogglesUsingInboxMembership(t *testing.T) {
	r, _, db := pendingAppFixture(t)
	service := &MessageService{Runtime: r}
	result, err := service.Archive(t.Context(), ArchiveRequest{MessageID: "a:m"})
	if err != nil || result != "archived" {
		t.Fatal(result, err)
	}
	ops, _ := db.PendingMailOps(t.Context(), "a", 10)
	if len(ops) != 1 || ops[0].OpType != "archive" {
		t.Fatal(ops)
	}
	// Removing the inbox membership models the committed archive.
	if err := db.QueueMessageOps(t.Context(), []store.PendingMailOpInput{{AccountID: "a", MessageID: "a:m", OpType: "archive", Payload: map[string]any{"remote_id": "remote-m"}}}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "DELETE FROM message_folders WHERE message_id='a:m'")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err = service.Archive(t.Context(), ArchiveRequest{MessageID: "a:m"})
	if err != nil || result != "unarchived" {
		t.Fatal("archive did not toggle back", result, err)
	}
}

func TestMoveAndArchiveValidateFolderOwnership(t *testing.T) {
	r, _, db := pendingAppFixture(t)
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Folders: []domain.Folder{{ID: "a:label:Work", AccountID: "a", RemoteID: "Label_work", Name: "Work", FolderType: "label"}}}); err != nil {
		t.Fatal(err)
	}
	service := &MessageService{Runtime: r}
	if _, err := service.Move(t.Context(), MoveRequest{MessageID: "a:m", TargetFolderID: "a:label:Work"}); err != nil {
		t.Fatal(err)
	}
	ops, _ := db.PendingMailOps(t.Context(), "a", 10)
	if len(ops) != 1 || ops[0].OpType != "move_to_folder" {
		t.Fatal(ops)
	}
	var payload map[string]any
	_, payloads, _ := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix())
	_ = json.Unmarshal(payloads[ops[0].ID], &payload)
	add, _ := payload["add_labels"].([]any)
	if len(add) != 1 || add[0] != "Label_work" {
		t.Fatal(payload)
	}
	if _, err := service.Move(t.Context(), MoveRequest{MessageID: "a:m", TargetFolderID: "missing"}); err == nil {
		t.Fatal("missing folder accepted")
	}
}
