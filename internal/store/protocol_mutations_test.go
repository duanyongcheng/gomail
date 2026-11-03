package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProtocolMoveUsesActualRolesAndInterruptedClaimIsUnknown(t *testing.T) {
	db := mutationFixture(t)
	execute(t, db.write, "UPDATE accounts SET provider='imap' WHERE id='a'")
	execute(t, db.write, "UPDATE folders SET role='inbox' WHERE account_id='a' AND remote_id='INBOX'")
	execute(t, db.write, `INSERT INTO folders(id,account_id,remote_id,name,folder_type,role,is_system) VALUES('trash-a','a','Deleted Items','Trash','folder','trash',1)`)
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "delete"})
	requireMembership(t, db, "a:m", "Deleted Items", true)
	requireMembership(t, db, "a:m", "INBOX", false)
	if integer(t, db.write, "SELECT COUNT(*) FROM folders WHERE account_id='a' AND remote_id='TRASH'") != 0 {
		t.Fatal("protocol mutation invented Gmail trash")
	}
	ops, payloads, err := db.ClaimPendingOps(t.Context(), 1, time.Now().Unix())
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	var payload MailOpPayload
	if err := json.Unmarshal(payloads[ops[0].ID], &payload); err != nil || payload.TargetRemoteID != "Deleted Items" {
		t.Fatal(payload, err)
	}
	if err := db.ResetInterruptedPendingOps(t.Context()); err != nil {
		t.Fatal(err)
	}
	ops, _, err = db.ClaimPendingOps(t.Context(), 1, time.Now().Add(time.Hour).Unix())
	if err != nil || len(ops) != 0 {
		t.Fatal("uncertain protocol move replayed", ops, err)
	}
	pending, err := db.PendingMailOps(t.Context(), "a", 10)
	if err != nil || len(pending) != 1 || pending[0].Result != "outcome_unknown" {
		t.Fatal(pending, err)
	}
}

func TestPOP3MutationRejectsBeforeLocalEffect(t *testing.T) {
	db := mutationFixture(t)
	execute(t, db.write, "UPDATE accounts SET provider='pop3' WHERE id='a'")
	read := true
	if _, err := db.QueueMailMutation(t.Context(), MailMutation{MessageIDs: []string{"a:m"}, OpType: "update_flags", IsRead: &read}); err == nil {
		t.Fatal("POP3 flags queued")
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM pending_mail_ops") != 0 {
		t.Fatal("rejected operation persisted")
	}
	requireMembership(t, db, "a:m", "INBOX", true)
}

func TestMembershipTombstonePreservesMoveIntentAndReceipt(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "success-receipt"}[completed], func(t *testing.T) {
			db := mutationFixture(t)
			execute(t, db.write, "UPDATE accounts SET provider='outlook' WHERE id='a'")
			execute(t, db.write, `INSERT INTO folders(id,account_id,remote_id,name,folder_type,role) VALUES('trash-a','a','Deleted Items','Trash','folder','trash')`)
			queueMutation(t, db, MailMutation{MessageIDs: []string{"a:m"}, OpType: "delete"})
			if completed {
				ops, _, err := db.ClaimPendingOps(t.Context(), 1, time.Now().Unix())
				if err != nil || len(ops) != 1 {
					t.Fatal(ops, err)
				}
				if err := db.CompletePendingOp(t.Context(), ops[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			watermark, err := db.PendingOpWatermark(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: "a", PendingOpWatermark: &watermark, RemovedMemberships: []RemovedMembership{{MessageID: "a:m", FolderID: "trash-a"}}}); err != nil {
				t.Fatal(err)
			}
			requireMembership(t, db, "a:m", "Deleted Items", true)
			requireMembership(t, db, "a:m", "INBOX", false)
			if completed && integer(t, db.write, "SELECT COUNT(*) FROM mail_op_sequence WHERE kind='succeeded'") != 1 {
				t.Fatal("folder absence retired global success receipt")
			}
		})
	}
}
