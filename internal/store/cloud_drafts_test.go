package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func TestCloudDraftMigrationAndJournal(t *testing.T) {
	raw := openRaw(t)
	if err := migrate(t.Context(), raw, 29); err != nil {
		t.Fatal(err)
	}
	execute(t, raw, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1)`)
	execute(t, raw, `INSERT INTO compose_drafts(id,account_id,request_key,data,revision,deleted,updated_at) VALUES('d','a','key',X'01',1,0,1)`)
	if err := migrate(t.Context(), raw, 30); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, raw)
	if integer(t, raw, "SELECT count(*) FROM compose_drafts") != 1 {
		t.Fatal("migration lost drafts")
	}
	db := openStore(t)
	ctx := t.Context()
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(ctx, domain.Account{ID: id, Email: id, Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SaveComposeDraft(ctx, EncryptedDraft{ID: "d", AccountID: "a", RequestKey: "key", Data: []byte("encrypted")}, nil); err != nil {
		t.Fatal(err)
	}
	pending := GmailDraftCopy{DraftID: "d", AccountID: "a", Revision: 1, State: "pending", Data: []byte("encrypted intent")}
	wrong := pending
	wrong.AccountID = "b"
	if err := db.BeginGmailDraftCopy(ctx, wrong); err == nil {
		t.Fatal("account mismatch accepted")
	}
	wrong = pending
	wrong.Revision = 2
	if err := db.BeginGmailDraftCopy(ctx, wrong); !errors.Is(err, ErrDraftConflict) {
		t.Fatal(err)
	}
	if err := db.BeginGmailDraftCopy(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginGmailDraftCopy(ctx, pending); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("duplicate intent", err)
	}
	if err := db.DeleteComposeDraft(ctx, "a", "d"); err == nil {
		t.Fatal("deleted draft with an unresolved write")
	}
	if _, err := db.GmailDraftCopy(ctx, "b", "d"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-account read", err)
	}
	path := db.Path()
	db.Close()
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.GmailDraftCopy(ctx, "a", "d")
	if err != nil || got.State != "pending" {
		t.Fatal(got, err)
	}
	if err := db.BeginGmailDraftCopy(ctx, pending); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("restart replayed intent", err)
	}
	got.State = "synced"
	got.RemoteID = "remote"
	got.RemoteMessageID = "message"
	wrong = got
	wrong.Data = []byte("stale receipt")
	if err := db.SettleGmailDraftCopy(ctx, wrong); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("stale receipt accepted", err)
	}
	if err := db.SettleGmailDraftCopy(ctx, got); err != nil {
		t.Fatal(err)
	}
	next := got
	next.Data = []byte("next encrypted intent")
	if err := db.BeginGmailDraftCopy(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := db.RejectGmailDraftCopy(ctx, next, &got); err != nil {
		t.Fatal(err)
	}
	restored, err := db.GmailDraftCopy(ctx, "a", "d")
	if err != nil || restored.State != "synced" || string(restored.Data) != string(got.Data) {
		t.Fatal(restored, err)
	}
	if err := db.DeleteAccount(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GmailDraftCopy(ctx, "a", "d"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("did not cascade", err)
	}
}
