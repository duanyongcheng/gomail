package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/store"
)

// Fault injection uses a second connection to the disposable test DB, never a
// production API for executing SQL.
func injectPendingSQL(t *testing.T, db *store.DB, query string) {
	t.Helper()
	conn, err := sql.Open("sqlite", db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func TestSettlementFailureRetriesOnlyDatabaseNotRemote(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			r, writer, db := pendingAppFixture(t)
			if outcome == "failure" {
				writer.fail = errors.New("private remote diagnostics")
			}
			if outcome == "unknown" {
				writer.unknown = true
			}
			if _, err := (&MessageService{Runtime: r}).Archive(t.Context(), ArchiveRequest{MessageID: "a:m"}); err != nil {
				t.Fatal(err)
			}
			injectPendingSQL(t, db, `CREATE TRIGGER fail_settlement BEFORE UPDATE OF status ON pending_mail_ops WHEN OLD.status='in_progress' AND NEW.status IN ('done','failed') BEGIN SELECT RAISE(ABORT,'injected'); END`)
			for range 2 {
				if _, err := r.processPendingOps(t.Context()); err == nil {
					t.Fatal("settlement failure hidden")
				}
			}
			writer.mu.Lock()
			calls := len(writer.calls)
			writer.mu.Unlock()
			if calls != 1 || r.pendingSettlement == nil {
				t.Fatal("remote replayed on DB failure", calls)
			}
			injectPendingSQL(t, db, "DROP TRIGGER fail_settlement")
			if _, err := r.processPendingOps(t.Context()); err != nil {
				t.Fatal(err)
			}
			writer.mu.Lock()
			calls = len(writer.calls)
			writer.mu.Unlock()
			if calls != 1 || r.pendingSettlement != nil {
				t.Fatal("settlement reissued remote request", calls)
			}
			ops, err := db.PendingMailOps(t.Context(), "a", 10)
			if err != nil {
				t.Fatal(err)
			}
			if outcome == "success" && len(ops) != 0 {
				t.Fatal(ops)
			}
			if outcome != "success" {
				if len(ops) != 1 || ops[0].Status != "failed" || ops[0].Attempts != 1 || ops[0].LastError == nil || strings.Contains(*ops[0].LastError, "private") {
					t.Fatal(ops)
				}
			}
		})
	}
}

func TestWorkerDrainsMoreThanOnePageWithoutPollingDelay(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	snapshot := store.Snapshot{AccountID: "a"}
	ids := []string{}
	for i := 0; i < 45; i++ {
		id := fmt.Sprintf("a:bulk%d", i)
		ids = append(ids, id)
		snapshot.Messages = append(snapshot.Messages, store.CachedMessage{Message: domain.Message{ID: id, AccountID: "a", RemoteID: fmt.Sprint(i)}, FolderIDs: []string{"a:label:INBOX"}})
	}
	if err := db.CommitSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := (&MessageService{Runtime: r}).BatchArchive(t.Context(), BatchRequest{MessageIDs: ids}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); r.RunPendingOpsWorker(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		summary, err := db.PendingMailOpsSummary(t.Context(), "a")
		if err != nil {
			t.Fatal(err)
		}
		if summary.TotalActiveCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("backlog waited for poll", summary)
		}
		time.Sleep(10 * time.Millisecond)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.calls) != 45 {
		t.Fatal(len(writer.calls))
	}
}

func TestRecoveryFailureBlocksRemoteUntilRecoverySucceeds(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	if _, err := (&MessageService{Runtime: r}).Archive(t.Context(), ArchiveRequest{MessageID: "a:m"}); err != nil {
		t.Fatal(err)
	}
	ops, _, err := db.ClaimPendingOps(t.Context(), 1, time.Now().Unix())
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	injectPendingSQL(t, db, `CREATE TRIGGER fail_recovery BEFORE UPDATE OF status ON pending_mail_ops WHEN NEW.status='pending' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); r.RunPendingOpsWorker(ctx) }()
	defer func() { cancel(); <-done }()
	time.Sleep(100 * time.Millisecond)
	writer.mu.Lock()
	calls := len(writer.calls)
	writer.mu.Unlock()
	if calls != 0 {
		t.Fatal("replayed despite failed recovery")
	}
	injectPendingSQL(t, db, "DROP TRIGGER fail_recovery")
	r.notifyPendingOps()
	deadline := time.Now().Add(3 * time.Second)
	for {
		summary, err := db.PendingMailOpsSummary(t.Context(), "a")
		if err != nil {
			t.Fatal(err)
		}
		if summary.TotalActiveCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(summary)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDismissWakesBlockedSuccessor(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	writer.unknown = true
	s := &MessageService{Runtime: r}
	if _, err := s.Archive(t.Context(), ArchiveRequest{MessageID: "a:m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(t.Context(), ArchiveRequest{MessageID: "a:m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.processPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-r.pendingWake
	if _, err := (&PendingOpsService{Runtime: r}).Dismiss(t.Context(), PendingOpsRequest{AccountID: "a"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.pendingWake:
	default:
		t.Fatal("dismiss did not wake successor")
	}
	writer.unknown = false
	if _, err := r.processPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	ops, _ := db.PendingMailOps(t.Context(), "a", 10)
	if len(ops) != 0 {
		t.Fatal(ops)
	}
	if len(writer.calls) != 2 || !strings.HasPrefix(writer.calls[1], "restore:") {
		t.Fatal(writer.calls)
	}
}

func TestSequentialTogglesReplayPersistedLabelIntent(t *testing.T) {
	r, writer, db := pendingAppFixture(t)
	s := &MessageService{Runtime: r}
	if _, err := s.Archive(t.Context(), ArchiveRequest{MessageID: "a:m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(t.Context(), ArchiveRequest{MessageID: "a:m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.processPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 2 {
		t.Fatal(writer.calls)
	}
	for i, want := range []string{"archive", "restore"} {
		kind, raw, _ := strings.Cut(writer.calls[i], ":")
		var payload store.MailOpPayload
		if json.Unmarshal([]byte(raw), &payload) != nil || kind != want || payload.RemoteID != "remote-m" {
			t.Fatal(writer.calls)
		}
	}
	inInbox, err := db.MessageInFolderRemote(t.Context(), "a:m", "INBOX")
	if err != nil || !inInbox {
		t.Fatal(inInbox, err)
	}
}

func TestEveryBatchMutationWakesPendingWorker(t *testing.T) {
	for _, kind := range []string{"flags", "account-read", "folder-read"} {
		t.Run(kind, func(t *testing.T) {
			r, _, _ := pendingAppFixture(t)
			s := &MessageService{Runtime: r}
			var err error
			switch kind {
			case "flags":
				read := true
				_, err = s.BatchFlags(t.Context(), BatchRequest{MessageIDs: []string{"a:m"}, IsRead: &read})
			case "account-read":
				_, err = s.MarkAccountAllRead(t.Context(), AccountRequest{AccountID: "a"})
			case "folder-read":
				_, err = s.MarkFolderAllRead(t.Context(), AllReadRequest{AccountID: "a", FolderIDs: []string{"a:label:INBOX"}})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-r.pendingWake:
			default:
				t.Fatal("mutation did not wake worker")
			}
		})
	}
}
