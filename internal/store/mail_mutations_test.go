package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
)

func mutationFixture(t *testing.T) *DB {
	t.Helper()
	db := pendingFixture(t)
	execute(t, db.write, "INSERT INTO message_folders(message_id,folder_id) VALUES('a:m','a:label:INBOX'),('b:m','b:label:INBOX')")
	return db
}
func queueMutation(t *testing.T, db *DB, request MailMutation) MailMutationResult {
	t.Helper()
	result, err := db.QueueMailMutation(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func requireMembership(t *testing.T, db *DB, message, remote string, want bool) {
	t.Helper()
	got, err := db.MessageInFolderRemote(t.Context(), message, remote)
	if err != nil || got != want {
		t.Fatalf("%s/%s = %v want %v: %v", message, remote, got, want, err)
	}
}

func TestMutationLocalFoldersAtomicityAndOwnership(t *testing.T) {
	db := mutationFixture(t)
	ctx := t.Context()
	execute(t, db.write, `CREATE TRIGGER fail_local BEFORE DELETE ON message_folders BEGIN SELECT RAISE(ABORT,'local failure'); END`)
	if _, err := db.QueueMailMutation(ctx, MailMutation{MessageIDs: []string{"a:m"}, OpType: "archive"}); err == nil {
		t.Fatal("failure ignored")
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM pending_mail_ops") != 0 || integer(t, db.write, "SELECT COUNT(*) FROM mail_op_sequence") != 0 {
		t.Fatal("failed local change left queued op")
	}
	requireMembership(t, db, "a:m", "INBOX", true)
	execute(t, db.write, "DROP TRIGGER fail_local")
	execute(t, db.write, `CREATE TRIGGER fail_queue BEFORE INSERT ON mail_op_sequence BEGIN SELECT RAISE(ABORT,'sequence failure'); END`)
	if _, err := db.QueueMailMutation(ctx, MailMutation{MessageIDs: []string{"a:m"}, OpType: "delete"}); err == nil {
		t.Fatal("sequence failure ignored")
	}
	requireMembership(t, db, "a:m", "INBOX", true)
	if integer(t, db.write, "SELECT COUNT(*) FROM pending_mail_ops") != 0 {
		t.Fatal("sequence rollback lost")
	}
	execute(t, db.write, "DROP TRIGGER fail_queue")
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "delete"})
	requireMembership(t, db, "a:m", "TRASH", true)
	requireMembership(t, db, "a:m", "INBOX", false)
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "restore"})
	requireMembership(t, db, "a:m", "TRASH", false)
	requireMembership(t, db, "a:m", "INBOX", true)
	execute(t, db.write, `INSERT INTO folders(id,account_id,remote_id,name,folder_type) VALUES('work','a','Work','Work','label')`)
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "move_to_folder", TargetFolderID: "work"})
	requireMembership(t, db, "a:m", "Work", true)
	requireMembership(t, db, "a:m", "INBOX", false)
	before := integer(t, db.write, "SELECT COUNT(*) FROM pending_mail_ops")
	if _, err := db.QueueMailMutation(ctx, MailMutation{MessageIDs: []string{"a:m", "b:m"}, OpType: "move_to_folder", TargetFolderID: "work"}); err == nil {
		t.Fatal("cross-account target accepted")
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM pending_mail_ops") != before {
		t.Fatal("partial batch queued")
	}
	requireMembership(t, db, "b:m", "INBOX", true)
	result := queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m", "a:m", "missing"}, OpType: "archive"})
	if result.Count != 1 {
		t.Fatal("duplicates/missing should not inflate count", result)
	}
}

func TestMutationFIFOBackoffUnknownAndReopen(t *testing.T) {
	db := mutationFixture(t)
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "archive"})
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "restore"})
	queueMutation(t, db, MailMutation{MessageIDs: []string{"b:m"}, OpType: "archive"})
	execute(t, db.write, "UPDATE pending_mail_ops SET created_at=1,updated_at=1")
	ops, _, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix())
	if err != nil || len(ops) != 2 || ops[0].AccountID != "a" || ops[0].OpType != "archive" {
		t.Fatal(ops, err)
	}
	if err := db.FailPendingOp(t.Context(), ops[0].ID, "offline", false); err != nil {
		t.Fatal(err)
	}
	if err := db.CompletePendingOp(t.Context(), ops[1].ID); err != nil {
		t.Fatal(err)
	}
	if got, _, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix()); err != nil || len(got) != 0 {
		t.Fatal("newer restore bypassed backoff", got, err)
	}
	ops, _, err = db.ClaimPendingOps(t.Context(), 10, time.Now().Add(time.Hour).Unix())
	if err != nil || len(ops) != 1 || ops[0].OpType != "archive" {
		t.Fatal(ops, err)
	}
	if err := db.FailPendingOp(t.Context(), ops[0].ID, "unknown", true); err != nil {
		t.Fatal(err)
	}
	other, err := Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.ResetInterruptedPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if got, _, err := other.ClaimPendingOps(t.Context(), 10, time.Now().Add(48*time.Hour).Unix()); err != nil || len(got) != 0 {
			t.Fatal("unknown or its successor retried", got, err)
		}
	}
	if n, err := other.RetryFailedPendingOps(t.Context(), "a"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if n, err := other.DismissFailedPendingOps(t.Context(), "a"); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	ops, _, err = other.ClaimPendingOps(t.Context(), 10, time.Now().Unix())
	if err != nil || len(ops) != 1 || ops[0].OpType != "restore" {
		t.Fatal("dismiss did not release successor", ops, err)
	}
}

func TestConcurrentTogglesAndClaimsHaveStableOrder(t *testing.T) {
	db := mutationFixture(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_, err := db.QueueMailMutation(t.Context(), MailMutation{MessageIDs: []string{"a:m"}, OpType: "archive", ToggleArchive: true})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	requireMembership(t, db, "a:m", "INBOX", true)
	for i := 0; i < 10; i++ {
		var mu sync.Mutex
		var claimed []PendingMailOp
		for range 3 {
			wg.Go(func() {
				ops, _, err := db.ClaimPendingOps(t.Context(), 100, time.Now().Unix())
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				claimed = append(claimed, ops...)
				mu.Unlock()
			})
		}
		wg.Wait()
		want := "archive"
		if i%2 == 1 {
			want = "restore"
		}
		if len(claimed) != 1 || claimed[0].OpType != want {
			t.Fatal(i, claimed)
		}
		if err := db.CompletePendingOp(t.Context(), claimed[0].ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncOverlayProtectsActiveAndSuccessUntilFreshRead(t *testing.T) {
	db := mutationFixture(t)
	ctx := t.Context()
	staleWatermark, _ := db.PendingOpWatermark(ctx)
	read, star := true, true
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "update_flags", IsRead: &read, IsStarred: &star})
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "archive"})
	m, _ := db.Message(ctx, "a:m")
	m.IsRead, m.IsStarred, m.BodyText = false, false, "new remote body"
	incoming := Snapshot{AccountID: "a", Messages: []CachedMessage{{Message: m, FolderIDs: []string{"a:label:INBOX"}}}, PendingOpWatermark: &staleWatermark}
	if err := db.CommitSnapshot(ctx, incoming); err != nil {
		t.Fatal(err)
	}
	assertOverlay := func() {
		t.Helper()
		got, err := db.Message(ctx, "a:m")
		if err != nil || !got.IsRead || !got.IsStarred || got.BodyText != "new remote body" {
			t.Fatal(got, err)
		}
		requireMembership(t, db, "a:m", "INBOX", false)
	}
	assertOverlay()
	for range 2 {
		ops, _, err := db.ClaimPendingOps(ctx, 1, time.Now().Unix())
		if err != nil || len(ops) != 1 {
			t.Fatal(ops, err)
		}
		if err := db.CompletePendingOp(ctx, ops[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CommitSnapshot(ctx, incoming); err != nil {
		t.Fatal(err)
	}
	assertOverlay() // Old read after remote success must still preserve intents.
	if integer(t, db.write, "SELECT COUNT(*) FROM mail_op_sequence WHERE kind='succeeded'") != 2 {
		t.Fatal("receipts prematurely removed")
	}
	watermark, _ := db.PendingOpWatermark(ctx)
	incoming.PendingOpWatermark = &watermark
	// Now a later real read is authoritative (including another client's edits).
	if err := db.CommitSnapshot(ctx, incoming); err != nil {
		t.Fatal(err)
	}
	got, _ := db.Message(ctx, "a:m")
	if got.IsRead || got.IsStarred {
		t.Fatal("completed intent permanently masked external flags")
	}
	requireMembership(t, db, "a:m", "INBOX", true)
	if integer(t, db.write, "SELECT COUNT(*) FROM mail_op_sequence WHERE kind='succeeded'") != 0 {
		t.Fatal("receipt not reconciled")
	}
}

func TestResultPersistenceAndRecoveryAreAtomic(t *testing.T) {
	db := mutationFixture(t)
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "archive"})
	ops, _, _ := db.ClaimPendingOps(t.Context(), 1, time.Now().Unix())
	execute(t, db.write, `CREATE TRIGGER reject_done BEFORE UPDATE OF status ON pending_mail_ops WHEN NEW.status='done' BEGIN SELECT RAISE(ABORT,'disk failure'); END`)
	if err := db.CompletePendingOp(t.Context(), ops[0].ID); err == nil {
		t.Fatal("failure swallowed")
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM mail_op_sequence WHERE kind='succeeded'") != 0 {
		t.Fatal("receipt committed without status")
	}
	execute(t, db.write, "DROP TRIGGER reject_done")
	// A legacy success marker on an interrupted send is completion-only.
	execute(t, db.write, `UPDATE pending_mail_ops SET op_type='send',payload_json=json_set(payload_json,'$._result','remote_succeeded') WHERE id=?`, ops[0].ID)
	if err := db.ResetInterruptedPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, _, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix()); err != nil || len(got) != 0 {
		t.Fatal("confirmed send requeued", got, err)
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM mail_op_sequence WHERE kind='succeeded'") != 1 {
		t.Fatal("recovery lost receipt")
	}
}

func TestV24SequenceUpgradeAndCascade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.sqlite")
	raw, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err := migrate(t.Context(), raw, 23); err != nil {
		t.Fatal(err)
	}
	execute(t, raw, `INSERT INTO pending_mail_ops(id,account_id,message_id,op_type,payload_json,status,created_at,updated_at) VALUES('z-first','a','m','archive','{}','pending',1,9),('a-second','a','m','restore','{}','pending',1,1)`)
	if err := migrate(t.Context(), raw, 24); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, raw)
	var first string
	if err := raw.QueryRow("SELECT op_id FROM mail_op_sequence ORDER BY sequence LIMIT 1").Scan(&first); err != nil || first != "z-first" {
		t.Fatal(first, err)
	}
	execute(t, raw, "DELETE FROM pending_mail_ops WHERE id='z-first'")
	if integer(t, raw, "SELECT COUNT(*) FROM mail_op_sequence") != 1 {
		t.Fatal("sequence did not cascade")
	}
}

func TestQueueOverlayRollbackAndConfirmedDeletion(t *testing.T) {
	db := mutationFixture(t)
	ctx := t.Context()
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "archive"})
	ops, _, _ := db.ClaimPendingOps(ctx, 1, time.Now().Unix())
	if err := db.CompletePendingOp(ctx, ops[0].ID); err != nil {
		t.Fatal(err)
	}
	watermark, _ := db.PendingOpWatermark(ctx)
	m, _ := db.Message(ctx, "a:m")
	execute(t, db.write, `CREATE TRIGGER reject_checkpoint BEFORE UPDATE OF sync_state ON accounts BEGIN SELECT RAISE(ABORT,'checkpoint failure'); END`)
	cursor := "10"
	if err := db.CommitSnapshot(ctx, Snapshot{AccountID: "a", Messages: []CachedMessage{{Message: m, FolderIDs: []string{"a:label:INBOX"}}}, PendingOpWatermark: &watermark, Cursor: &cursor}); err == nil {
		t.Fatal("checkpoint failure ignored")
	}
	requireMembership(t, db, "a:m", "INBOX", false)
	if integer(t, db.write, "SELECT COUNT(*) FROM mail_op_sequence WHERE kind='succeeded'") != 1 {
		t.Fatal("receipt release not rolled back")
	}
	execute(t, db.write, "DROP TRIGGER reject_checkpoint")
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "restore"})
	if err := db.CommitSnapshot(ctx, Snapshot{AccountID: "a", Removed: []string{"a:m"}}); err != nil {
		t.Fatal(err)
	}
	if got, _, err := db.ClaimPendingOps(ctx, 10, time.Now().Unix()); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if _, err := db.Message(ctx, "a:m"); err != sql.ErrNoRows {
		t.Fatal("confirmed deletion lost", err)
	}
}

func TestRecoveryRollsBackAllRowsOnFailure(t *testing.T) {
	db := mutationFixture(t)
	for _, account := range []string{"a", "b"} {
		if err := db.QueueMessageOps(t.Context(), []PendingMailOpInput{{AccountID: account, MessageID: account + ":m", OpType: "send", Payload: MailOpPayload{RemoteID: "m"}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	ops, _, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix())
	if err != nil || len(ops) != 2 {
		t.Fatal(ops, err)
	}
	execute(t, db.write, `CREATE TRIGGER recovery_failure BEFORE UPDATE OF status ON pending_mail_ops WHEN NEW.account_id='b' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.ResetInterruptedPendingOps(t.Context()); err == nil {
		t.Fatal("recovery failure ignored")
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM pending_mail_ops WHERE status='in_progress'") != 2 {
		t.Fatal("recovery partially committed")
	}
}

func TestStartupRepairsLegacyQueuedLocalEffects(t *testing.T) {
	db := mutationFixture(t)
	// Models the old queue-only folder change in a profile reopened offline.
	if err := db.QueueMessageOps(t.Context(), []PendingMailOpInput{{AccountID: "a", MessageID: "a:m", OpType: "archive", Payload: MailOpPayload{RemoteID: "m", RemoveLabels: []string{"INBOX"}}}}, nil); err != nil {
		t.Fatal(err)
	}
	requireMembership(t, db, "a:m", "INBOX", true)
	if err := db.ResetInterruptedPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireMembership(t, db, "a:m", "INBOX", false)
}

func TestUnknownMarkerIsExcludedEvenWithPendingStatus(t *testing.T) {
	db := mutationFixture(t)
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "archive"})
	execute(t, db.write, `UPDATE pending_mail_ops SET payload_json=json_set(payload_json,'$._result','outcome_unknown')`)
	if ops, _, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix()); err != nil || len(ops) != 0 {
		t.Fatal(ops, err)
	}
}

func TestPersistedMutationSurvivesClosedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.sqlite")
	ctx := context.Background()
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSnapshot(ctx, Snapshot{AccountID: "a", Messages: []CachedMessage{{Message: domain.Message{ID: "m", AccountID: "a", RemoteID: "r"}}}}); err != nil {
		t.Fatal(err)
	}
	queueMutation(t, db, MailMutation{MessageIDs: []string{"m"}, OpType: "delete"})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	requireMembership(t, db, "m", "TRASH", true)
	ops, payloads, err := db.ClaimPendingOps(ctx, 1, time.Now().Unix())
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	var p MailOpPayload
	if err := json.Unmarshal(payloads[ops[0].ID], &p); err != nil || p.RemoteID != "r" || len(p.AddLabels) != 1 || p.AddLabels[0] != "TRASH" {
		t.Fatal(p, err)
	}
}
