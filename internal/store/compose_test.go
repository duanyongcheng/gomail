package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func TestComposeSaveRollbackAndRevisionCAS(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.StageComposeAttachment(ctx, EncryptedComposeAttachment{ID: "file", Data: []byte("encrypted attachment"), Size: 3}); err != nil {
		t.Fatal(err)
	}
	d := EncryptedDraft{ID: "d", AccountID: "a", RequestKey: "key", Data: []byte("old encrypted draft")}
	if err := db.SaveComposeDraft(ctx, d, []string{"file"}); err != nil {
		t.Fatal(err)
	}
	d, err := db.ComposeDraft(ctx, "a", "d")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReleaseComposeAttachment(ctx, "file"); err != nil {
		t.Fatal(err)
	}
	execute(t, db.write, `CREATE TRIGGER fail_draft_refs BEFORE DELETE ON compose_draft_attachments BEGIN SELECT RAISE(ABORT,'injected'); END`)
	d.Data = []byte("new encrypted draft")
	if err := db.SaveComposeDraft(ctx, d, nil); err == nil {
		t.Fatal("failure ignored")
	}
	got, err := db.ComposeDraft(ctx, "a", "d")
	if err != nil || string(got.Data) != "old encrypted draft" || got.Revision != 1 {
		t.Fatal(got, err)
	}
	if _, err := db.ComposeAttachment(ctx, "file"); err != nil {
		t.Fatal("rollback deleted attachment", err)
	}
	execute(t, db.write, "DROP TRIGGER fail_draft_refs")
	if err := db.SaveComposeDraft(ctx, d, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ComposeAttachment(ctx, "file"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("removed reference leaked", err)
	}
	if err := db.SaveComposeDraft(ctx, d, nil); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("stale revision accepted", err)
	}
}
func TestComposeMigrationPreservesLegacyAndData(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 24); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1)`)
	if err := migrate(t.Context(), db, 25); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, db)
	if integer(t, db, "SELECT COUNT(*) FROM accounts") != 1 {
		t.Fatal("account lost")
	}
}
func TestComposeAttachmentLimits(t *testing.T) {
	db := openStore(t)
	if err := db.StageComposeAttachment(t.Context(), EncryptedComposeAttachment{ID: "x", Size: MaxComposeAttachmentBytes + 1, Data: []byte("cipher")}); err == nil {
		t.Fatal("per-file limit ignored")
	}
}
