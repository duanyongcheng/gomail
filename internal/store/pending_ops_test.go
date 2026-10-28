package store

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/domain"
)

func pendingFixture(t *testing.T) *DB {
	t.Helper()
	db := openStore(t)
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id, Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
			t.Fatal(err)
		}
		execute(t, db.write, `INSERT INTO folders(id,account_id,remote_id,name,folder_type) VALUES(?,?,?,?,?)`, id+":label:INBOX", id, "INBOX", "Inbox", "label")
		execute(t, db.write, `INSERT INTO messages(id,account_id,remote_id,subject,date,created_at,updated_at) VALUES(?,?,?,?,1,1,1)`, id+":m", id, "m", "subject")
	}
	return db
}

func TestPendingOpsQueueAndLocalChangeAreAtomic(t *testing.T) {
	db := pendingFixture(t)
	read := true
	op := PendingMailOpInput{AccountID: "a", MessageID: "a:m", OpType: "update_flags", Payload: map[string]any{"remote_id": "m", "remove_labels": []string{"UNREAD"}}}
	execute(t, db.write, `CREATE TRIGGER inject_pending_failure BEFORE INSERT ON pending_mail_ops BEGIN SELECT RAISE(ABORT,'injected'); END`)
	err := db.QueueMessageOps(t.Context(), []PendingMailOpInput{op}, func(tx *sql.Tx) error {
		return ApplyLocalMailChanges(t.Context(), tx, []LocalMailChange{{MessageID: "a:m", IsRead: &read}})
	})
	if err == nil {
		t.Fatal("injected failure accepted")
	}
	message, _ := db.Message(t.Context(), "a:m")
	if message.IsRead {
		t.Fatal("local change survived failed queue insert")
	}
	execute(t, db.write, "DROP TRIGGER inject_pending_failure")
	if err := db.QueueMessageOps(t.Context(), []PendingMailOpInput{op}, func(tx *sql.Tx) error {
		return ApplyLocalMailChanges(t.Context(), tx, []LocalMailChange{{MessageID: "a:m", IsRead: &read}})
	}); err != nil {
		t.Fatal(err)
	}
	message, _ = db.Message(t.Context(), "a:m")
	if !message.IsRead {
		t.Fatal("local change missing after commit")
	}
	listed, err := db.PendingMailOps(t.Context(), "a", 10)
	if err != nil || len(listed) != 1 || listed[0].Status != "pending" || listed[0].Result != "pending" {
		t.Fatal(listed, err)
	}
}

func TestPendingOpsClaimFailRetryAndSummary(t *testing.T) {
	db := pendingFixture(t)
	op := PendingMailOpInput{AccountID: "a", MessageID: "a:m", OpType: "archive", Payload: map[string]any{"remote_id": "m", "remove_labels": []string{"INBOX"}}}
	if err := db.QueueMessageOps(t.Context(), []PendingMailOpInput{op}, nil); err != nil {
		t.Fatal(err)
	}
	summary, err := db.PendingMailOpsSummary(t.Context(), "")
	if err != nil || summary.PendingCount != 1 || summary.TotalActiveCount != 1 {
		t.Fatal(summary, err)
	}
	claimed, payloads, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix())
	if err != nil || len(claimed) != 1 || len(payloads[claimed[0].ID]) == 0 {
		t.Fatal(claimed, payloads, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloads[claimed[0].ID], &payload); err != nil || payload["remote_id"] != "m" {
		t.Fatal(payload, err)
	}
	if err := db.FailPendingOp(t.Context(), claimed[0].ID, "offline", false); err != nil {
		t.Fatal(err)
	}
	ops, err := db.PendingMailOps(t.Context(), "a", 10)
	if err != nil || len(ops) != 1 || ops[0].Status != "failed" || ops[0].Attempts != 1 || ops[0].NextRetryAt == nil || *ops[0].NextRetryAt <= time.Now().Unix() {
		t.Fatal(ops, err)
	}
	claimed, _, err = db.ClaimPendingOps(t.Context(), 10, time.Now().Unix())
	if err != nil || len(claimed) != 0 {
		t.Fatal("retry claimed before backoff", claimed, err)
	}
	claimed, _, err = db.ClaimPendingOps(t.Context(), 10, time.Now().Add(2*time.Minute).Unix())
	if err != nil || len(claimed) != 1 {
		t.Fatal("due retry not claimed", claimed, err)
	}
	if err := db.CompletePendingOp(t.Context(), claimed[0].ID); err != nil {
		t.Fatal(err)
	}
	summary, err = db.PendingMailOpsSummary(t.Context(), "a")
	if err != nil || summary.TotalActiveCount != 0 {
		t.Fatal(summary, err)
	}
}

func TestPendingOpsInterruptionUnknownOutcomeAndDismiss(t *testing.T) {
	db := pendingFixture(t)
	ops := []PendingMailOpInput{
		{AccountID: "a", MessageID: "a:m", OpType: "archive", Payload: map[string]any{"remote_id": "m"}},
		{AccountID: "b", MessageID: "b:m", OpType: "send", Payload: map[string]any{"remote_id": "m"}},
	}
	if err := db.QueueMessageOps(t.Context(), ops, nil); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Unix())
	if err != nil || len(claimed) != 2 {
		t.Fatal(claimed, err)
	}
	if err := db.ResetInterruptedPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	listed, err := db.PendingMailOps(t.Context(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]PendingMailOp{}
	for _, op := range listed {
		byType[op.OpType] = op
	}
	if byType["archive"].Status != "pending" || byType["archive"].Result != "pending" {
		t.Fatal("interrupted non-send not replayed", byType["archive"])
	}
	if byType["send"].Status != "failed" || byType["send"].Result != "outcome_unknown" || byType["send"].NextRetryAt != nil {
		t.Fatal("interrupted send became retryable", byType["send"])
	}
	claimed, _, err = db.ClaimPendingOps(t.Context(), 10, time.Now().Add(48*time.Hour).Unix())
	if err != nil || len(claimed) != 1 || claimed[0].OpType != "archive" {
		t.Fatal("unknown send was retried", claimed, err)
	}
	if n, err := db.DismissFailedPendingOps(t.Context(), "b"); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	summary, _ := db.PendingMailOpsSummary(t.Context(), "")
	if summary.FailedCount != 0 || summary.PendingCount != 0 {
		t.Fatal(summary)
	}
}

func TestPendingOpsRetryBudgetAccountIsolationAndLegacyUpgrade(t *testing.T) {
	db := pendingFixture(t)
	for _, account := range []string{"a", "b"} {
		if err := db.QueueMessageOps(t.Context(), []PendingMailOpInput{{AccountID: account, MessageID: account + ":m", OpType: "archive", Payload: map[string]any{"remote_id": "m"}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < MaxPendingMailOpAttempts+1; i++ {
		claimed, _, err := db.ClaimPendingOps(t.Context(), 10, time.Now().Add(time.Duration(i)*2*time.Hour).Unix())
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range claimed {
			if err := db.FailPendingOp(t.Context(), op.ID, "offline", false); err != nil {
				t.Fatal(err)
			}
		}
	}
	listed, _ := db.PendingMailOps(t.Context(), "a", 10)
	if len(listed) != 1 || listed[0].Status != "failed" || listed[0].Attempts != MaxPendingMailOpAttempts || listed[0].NextRetryAt != nil {
		t.Fatal("retry budget", listed)
	}
	other, _ := db.PendingMailOps(t.Context(), "b", 10)
	if len(other) != 1 || other[0].AccountID != "b" {
		t.Fatal("account isolation", other)
	}
	raw := openRaw(t)
	if err := migrate(t.Context(), raw, 22); err != nil {
		t.Fatal(err)
	}
	execute(t, raw, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1)`)
	execute(t, raw, `INSERT INTO pending_mail_ops(id,account_id,message_id,op_type,payload_json,status,attempts,created_at,updated_at) VALUES('old','a','m','archive','{"remote_id":"m"}','pending',0,1,1)`)
	if err := migrate(t.Context(), raw, SchemaVersion); err != nil {
		t.Fatal(err)
	}
	var result string
	if err := raw.QueryRow("SELECT payload_json FROM pending_mail_ops WHERE id='old'").Scan(&result); err != nil || result != `{"remote_id":"m"}` {
		t.Fatal("legacy pending op changed", result, err)
	}
	checkLegacySchema(t, raw)
}
