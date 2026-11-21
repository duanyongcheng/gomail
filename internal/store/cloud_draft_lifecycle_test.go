package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func TestCloudLifecycleMigrationPreservesV30AndLegacy(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 30); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1)`)
	execute(t, db, `INSERT INTO compose_drafts(id,account_id,request_key,data,revision,deleted,updated_at) VALUES('local','a','key',X'01',1,0,1)`)
	execute(t, db, `INSERT INTO gmail_draft_copies(draft_id,account_id,remote_id,remote_message_id,revision,state,data) VALUES('local','a','remote','message',1,'pending',X'02')`)
	if err := migrate(t.Context(), db, 31); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, db)
	if integer(t, db, `SELECT count(*) FROM gmail_draft_copies WHERE state='pending' AND data=X'02' AND outbox_id IS NULL`) != 1 {
		t.Fatal("migration lost unresolved cloud state")
	}
}

func lifecycleStoreInput(id string) (EncryptedDraft, []EncryptedComposeAttachment, GmailDraftCopy) {
	d := EncryptedDraft{ID: id, AccountID: "a", RequestKey: "key-" + id, Data: []byte("encrypted draft")}
	a := []EncryptedComposeAttachment{{ID: "attachment-" + id, Data: []byte("encrypted bytes"), Size: 3}}
	c := GmailDraftCopy{DraftID: id, AccountID: "a", RemoteID: "remote", RemoteMessageID: "message", Revision: 1, State: "synced", Data: []byte("encrypted identity-" + id)}
	return d, a, c
}

func TestCloudImportAtomicRollbackQuotaAndPreservePrevious(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	d, attachments, copy := lifecycleStoreInput("first")
	execute(t, db.write, `CREATE TRIGGER abort_import BEFORE INSERT ON gmail_draft_copies BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.ImportGmailDraft(ctx, d, attachments, copy, nil); err == nil {
		t.Fatal("import fault ignored")
	}
	for _, table := range []string{"compose_drafts", "compose_attachments", "compose_draft_attachments", "gmail_draft_copies"} {
		if integer(t, db.write, "SELECT count(*) FROM "+table) != 0 {
			t.Fatalf("partial import in %s", table)
		}
	}
	execute(t, db.write, `DROP TRIGGER abort_import`)
	if err := db.ImportGmailDraft(ctx, d, attachments, copy, nil); err != nil {
		t.Fatal(err)
	}
	next, nextAttachments, nextCopy := lifecycleStoreInput("second")
	nextCopy.RemoteMessageID = "changed"
	copy.State = "conflict"
	if err := db.SettleGmailDraftCopy(ctx, copy); err != nil {
		t.Fatal(err)
	}
	// Failure after detaching must put the original association back.
	execute(t, db.write, `CREATE TRIGGER abort_import BEFORE INSERT ON compose_attachments BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.ImportGmailDraft(ctx, next, nextAttachments, nextCopy, &copy); err == nil {
		t.Fatal("transfer fault ignored")
	}
	if got, err := db.GmailDraftCopy(ctx, "a", "first"); err != nil || got.State != "conflict" {
		t.Fatal("association lost on failed transfer", got, err)
	}
	execute(t, db.write, `DROP TRIGGER abort_import`)
	if err := db.ImportGmailDraft(ctx, next, nextAttachments, nextCopy, &copy); err != nil {
		t.Fatal(err)
	}
	if got, err := db.ComposeDraft(ctx, "a", "first"); err != nil || got.Deleted || string(got.Data) != string(d.Data) {
		t.Fatal("conflict recovery changed old content", got, err)
	}
	if _, err := db.GmailDraftCopy(ctx, "a", "first"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old copy still bound", err)
	}
	if integer(t, db.write, "SELECT count(*) FROM compose_drafts") != 2 || integer(t, db.write, "SELECT count(*) FROM compose_attachments") != 2 {
		t.Fatal("did not keep both local versions")
	}
	oversized := []EncryptedComposeAttachment{{ID: "large", Size: MaxComposeAttachmentBytes + 1, Data: []byte("cipher")}}
	third, _, thirdCopy := lifecycleStoreInput("third")
	thirdCopy.RemoteID = "another"
	if err := db.ImportGmailDraft(ctx, third, oversized, thirdCopy, nil); err == nil {
		t.Fatal("attachment size limit ignored")
	}
}

func TestCloudImportCommitFailureLeavesNoPartialRecords(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	execute(t, db.write, `CREATE TABLE import_commit_fault(id INTEGER PRIMARY KEY, account_id TEXT REFERENCES accounts(id) DEFERRABLE INITIALLY DEFERRED)`)
	execute(t, db.write, `CREATE TRIGGER abort_import_commit AFTER INSERT ON gmail_draft_copies BEGIN INSERT INTO import_commit_fault(account_id) VALUES('missing'); END`)
	d, attachments, copy := lifecycleStoreInput("local")
	if err := db.ImportGmailDraft(ctx, d, attachments, copy, nil); err == nil {
		t.Fatal("commit failure ignored")
	}
	for _, table := range []string{"compose_drafts", "compose_attachments", "compose_draft_attachments", "gmail_draft_copies"} {
		if integer(t, db.write, "SELECT count(*) FROM "+table) != 0 {
			t.Fatalf("partial committed import in %s", table)
		}
	}
}

func TestCloudDeleteJournalCannotDetachUnknownOrCrossAccount(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	d, attachments, copy := lifecycleStoreInput("local")
	if err := db.ImportGmailDraft(ctx, d, attachments, copy, nil); err != nil {
		t.Fatal(err)
	}
	wrong := copy
	wrong.AccountID = "b"
	if err := db.BeginGmailDraftDelete(ctx, wrong); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("cross-account delete intent", err)
	}
	if err := db.BeginGmailDraftDelete(ctx, copy); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginGmailDraftDelete(ctx, copy); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("repeated delete intent", err)
	}
	if err := db.DeleteComposeDraft(ctx, "a", "local"); err == nil {
		t.Fatal("local deletion lost unknown identity")
	}
	pretendConflict := copy
	pretendConflict.State = "conflict"
	if err := db.RemoveGmailDraftCopy(ctx, pretendConflict); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("unknown state detached", err)
	}
	if err := db.RejectGmailDraftDelete(ctx, copy); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginGmailDraftDelete(ctx, copy); err != nil {
		t.Fatal(err)
	}
	copy.State = "delete_pending"
	if err := db.RemoveGmailDraftCopy(ctx, copy); err != nil {
		t.Fatal(err)
	}
	if got, err := db.ComposeDraft(ctx, "a", "local"); err != nil || got.Deleted {
		t.Fatal("remote delete removed local content", got, err)
	}
}
