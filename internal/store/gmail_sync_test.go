package store

import (
	"testing"

	"github.com/bary/pebble-go/internal/domain"
)

func TestGmailPageCheckpointCacheSearchAndReconciliationCommitTogether(t *testing.T) {
	db := openStore(t)
	execute(t, db.write, `INSERT INTO accounts(id,email,provider,created_at,updated_at,sync_state) VALUES
	('a','a','gmail',1,1,'{"custom":"preserve"}'),('b','b','gmail',1,1,'{}')`)
	execute(t, db.write, `INSERT INTO messages(id,account_id,remote_id,subject,date,created_at,updated_at) VALUES ('a:m','a','m','original',1,1,1),('b:m','b','m','other',1,1,1)`)
	checkpoint := &GmailCheckpoint{Phase: "messages", HistoryID: "900719925474099312345"}
	for _, account := range []string{"a", "b"} {
		if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: account, Checkpoint: checkpoint, ResetBackfill: true}); err != nil {
			t.Fatal(err)
		}
	}
	execute(t, db.write, `CREATE TRIGGER inject_checkpoint_failure BEFORE UPDATE OF sync_state ON accounts BEGIN SELECT RAISE(ABORT,'injected checkpoint failure'); END`)
	next := &GmailCheckpoint{Phase: "messages", HistoryID: checkpoint.HistoryID, PageToken: "next"}
	snapshot := Snapshot{AccountID: "a", Checkpoint: next, ProcessedRemoteIDs: []string{"m"}, Messages: []CachedMessage{{Message: domain.Message{ID: "a:m", AccountID: "a", RemoteID: "m", Subject: "replacement", To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}}}}}
	if err := db.CommitSnapshot(t.Context(), snapshot); err == nil {
		t.Fatal("injected transaction failure ignored")
	}
	state, err := db.GmailState(t.Context(), "a")
	if err != nil || state.Checkpoint.PageToken != "" {
		t.Fatal("failed transaction advanced checkpoint", state, err)
	}
	if integer(t, db.read, "SELECT count(*) FROM message_search WHERE message_search MATCH 'replacement'") != 0 || integer(t, db.read, "SELECT count(*) FROM gmail_backfill_pending WHERE account_id='a'") != 1 {
		t.Fatal("failed page changed FTS or reconciliation")
	}
	execute(t, db.write, "DROP TRIGGER inject_checkpoint_failure")
	if err := db.CommitSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	state, err = db.GmailState(t.Context(), "a")
	if err != nil || state.Checkpoint.PageToken != "next" {
		t.Fatal("checkpoint did not persist", state, err)
	}
	if integer(t, db.read, "SELECT count(*) FROM gmail_backfill_pending WHERE account_id='a'") != 0 || integer(t, db.read, "SELECT count(*) FROM gmail_backfill_pending WHERE account_id='b'") != 1 {
		t.Fatal("page reconciliation crossed accounts")
	}
	var custom string
	if err := db.read.QueryRow("SELECT json_extract(sync_state,'$.custom') FROM accounts WHERE id='a'").Scan(&custom); err != nil || custom != "preserve" {
		t.Fatal("checkpoint overwrote unrelated sync state", custom, err)
	}
	if err := db.DeleteAccount(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	if integer(t, db.read, "SELECT count(*) FROM gmail_backfill_pending") != 0 {
		t.Fatal("account cascade left reconciliation entries")
	}
}

func TestV21ProfileUpgradesToV22WithoutLosingSyncState(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 21); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at,sync_state) VALUES('a','a','gmail',1,1,'{"last_sync_cursor":"900719925474099312345"}')`)
	if err := migrate(t.Context(), db, SchemaVersion); err != nil {
		t.Fatal(err)
	}
	var cursor string
	if err := db.QueryRow("SELECT json_extract(sync_state,'$.last_sync_cursor') FROM accounts WHERE id='a'").Scan(&cursor); err != nil || cursor != "900719925474099312345" {
		t.Fatal("M0 upgrade lost cursor", cursor, err)
	}
	checkLegacySchema(t, db)
}
