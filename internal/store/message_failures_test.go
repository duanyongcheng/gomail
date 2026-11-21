package store

import (
	"testing"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func TestMessageFailuresCommitWithCheckpointAndPreserveExistingCache(t *testing.T) {
	db := openStore(t)
	execute(t, db.write, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1),('b','b','gmail',1,1)`)
	execute(t, db.write, `INSERT INTO messages(id,account_id,remote_id,subject,body_text,date,created_at,updated_at) VALUES('a:m','a','m','cached','keep searchable',1,1,1)`)
	checkpoint := &GmailCheckpoint{Phase: "messages", HistoryID: "123", PageToken: "next"}
	snapshot := Snapshot{AccountID: "a", Checkpoint: checkpoint, Failures: []MessageFailure{{RemoteID: "m", Kind: "invalid_content", NotifyOnRecovery: false}}}
	execute(t, db.write, `CREATE TRIGGER inject_failure_checkpoint BEFORE UPDATE OF sync_state ON accounts BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.CommitSnapshot(t.Context(), snapshot); err == nil {
		t.Fatal("checkpoint failure ignored")
	}
	page, err := db.DownloadFailures(t.Context(), "a", 20, 0)
	if err != nil || page.Total != 0 {
		t.Fatal("rollback retained failure record", page, err)
	}
	execute(t, db.write, "DROP TRIGGER inject_failure_checkpoint")
	if err := db.CommitSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	failure, err := db.MessageFailure(t.Context(), "a", "m")
	if err != nil || failure.Attempts != 1 || failure.NextRetryAt < time.Now().Add(4*time.Minute).Unix() || failure.Subject != "cached" {
		t.Fatal("failure record", failure, err)
	}
	message, err := db.Message(t.Context(), "a:m")
	if err != nil || message.BodyText != "keep searchable" {
		t.Fatal("failure damaged previous cache", message, err)
	}
	if integer(t, db.read, "SELECT count(*) FROM message_search WHERE message_search MATCH 'searchable'") != 1 {
		t.Fatal("failure removed cached FTS")
	}
	snapshot.Failures[0].NotifyOnRecovery = true
	if err := db.CommitSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	failure, _ = db.MessageFailure(t.Context(), "a", "m")
	if failure.Attempts != 2 || failure.NotifyOnRecovery {
		t.Fatal("repeat failure changed origin or lost attempts", failure)
	}
	if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: "b", Failures: []MessageFailure{{RemoteID: "m", Kind: "too_large"}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueueMessageFailures(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	ready, err := db.ReadyMessageFailures(t.Context(), "a", time.Now().Unix(), 10)
	if err != nil || len(ready) != 1 {
		t.Fatal("explicit retry was not queued", ready, err)
	}
	_, due, err := db.MessageFailureCounts(t.Context(), "b", time.Now().Unix())
	if err != nil || due != 0 {
		t.Fatal("retry crossed accounts", due, err)
	}
	if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: "a", RecoveredRemoteIDs: []string{"m"}, Messages: []CachedMessage{{Message: domain.Message{ID: "a:m", AccountID: "a", RemoteID: "m", Subject: "recovered", To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}}}}}); err != nil {
		t.Fatal(err)
	}
	page, _ = db.DownloadFailures(t.Context(), "", 20, 0)
	if page.Total != 1 || page.Items[0].AccountID != "b" {
		t.Fatal("recovery cleared another account", page)
	}
	if err := db.DeleteAccount(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	if integer(t, db.read, "SELECT count(*) FROM gmail_message_failures") != 0 {
		t.Fatal("account deletion did not cascade")
	}
}

func TestFailureRetryDelayAndV22Upgrade(t *testing.T) {
	for _, test := range []struct {
		attempt int
		want    time.Duration
	}{{1, 5 * time.Minute}, {2, 10 * time.Minute}, {3, 20 * time.Minute}, {8, 6 * time.Hour}, {1000000, 6 * time.Hour}} {
		if got := failureDelay(test.attempt); got != test.want {
			t.Fatal(test.attempt, got, test.want)
		}
	}
	db := openRaw(t)
	if err := migrate(t.Context(), db, 22); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at,sync_state) VALUES('a','a','gmail',1,1,'{"last_sync_cursor":"900719925474099312345","gmail_checkpoint":{"phase":"complete"}}')`)
	if err := migrate(t.Context(), db, SchemaVersion); err != nil {
		t.Fatal(err)
	}
	var cursor string
	if err := db.QueryRow("SELECT json_extract(sync_state,'$.last_sync_cursor') FROM accounts WHERE id='a'").Scan(&cursor); err != nil || cursor != "900719925474099312345" {
		t.Fatal("upgrade damaged cursor", cursor, err)
	}
	checkLegacySchema(t, db)
}
