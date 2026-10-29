package store

import (
	"testing"

	"github.com/bary/pebble-go/internal/domain"
)

func labelFoldersFixture(account string) []domain.Folder {
	return []domain.Folder{{ID: ScopedID(account, "label:INBOX"), AccountID: account, RemoteID: "INBOX", Name: "Inbox", FolderType: "label", IsSystem: true}, {ID: ScopedID(account, "label:work"), AccountID: account, RemoteID: "work", Name: "Work", FolderType: "label"}}
}
func TestLabelReconciliationScopeAndRollback(t *testing.T) {
	db := pendingFixture(t)
	ctx := t.Context()
	for _, account := range []string{"a", "b"} {
		if err := db.ReconcileGmailLabels(ctx, account, labelFoldersFixture(account)); err != nil {
			t.Fatal(err)
		}
	}
	execute(t, db.write, `INSERT INTO message_folders(message_id,folder_id) VALUES('a:m','a:label:work'),('b:m','b:label:work')`)
	changeID, err := db.BeginGmailLabelChange(ctx, "a", GmailLabelChange{Action: "delete", RemoteID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.QueueMailMutation(ctx, MailMutation{MessageIDs: []string{"a:m"}, OpType: "move_to_folder", TargetFolderID: "a:label:work"}); err == nil {
		t.Fatal("move accepted during label deletion")
	}
	execute(t, db.write, `CREATE TRIGGER fail_label_remove BEFORE DELETE ON folders WHEN OLD.remote_id='work' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.ReconcileGmailLabels(ctx, "a", labelFoldersFixture("a")[:1]); err == nil {
		t.Fatal("failure ignored")
	}
	if _, _, err := db.Folder(ctx, "a:label:work"); err != nil {
		t.Fatal("partial delete", err)
	}
	if pending, _ := db.GmailLabelChange(ctx, "a"); pending == nil || pending.ID != changeID {
		t.Fatal("marker lost")
	}
	execute(t, db.write, "DROP TRIGGER fail_label_remove")
	if err := db.ReconcileGmailLabels(ctx, "a", labelFoldersFixture("a")[:1]); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Folder(ctx, "b:label:work"); err != nil {
		t.Fatal("other account pruned", err)
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM messages") != 2 {
		t.Fatal("label delete removed mail")
	}
	if pending, _ := db.GmailLabelChange(ctx, "a"); pending != nil {
		t.Fatal("delete not reconciled")
	}
	if err := db.ReconcileGmailLabels(ctx, "b", nil); err == nil {
		t.Fatal("empty snapshot accepted")
	}
}
func TestUnmatchedLabelIntentAndStaleDismiss(t *testing.T) {
	db := pendingFixture(t)
	ctx := t.Context()
	id, err := db.BeginGmailLabelChange(ctx, "a", GmailLabelChange{Action: "create", Name: "New"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileGmailLabels(ctx, "a", labelFoldersFixture("a")); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearGmailLabelChange(ctx, "a", "stale-id"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := db.GmailLabelChange(ctx, "a"); pending == nil {
		t.Fatal("stale dismiss cleared new action")
	}
	if err := db.ClearGmailLabelChange(ctx, "a", id); err != nil {
		t.Fatal(err)
	}
}
func TestV28LabelMigrationPreservesOracle(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 27); err != nil {
		t.Fatal(err)
	}
	if err := migrate(t.Context(), db, 28); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, db)
}
