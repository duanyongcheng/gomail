package store

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/duanyongcheng/gomail/internal/domain"
)

func TestProviderCheckpointAtomicReopenAndAccountIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := db.CreateProtocolAccount(t.Context(), domain.Account{ID: id, Email: id + "@example.invalid", Provider: "imap"}, []byte("cipher")); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := Snapshot{AccountID: "a", Folders: []domain.Folder{{ID: "f", AccountID: "a", RemoteID: "INBOX", Name: "Inbox", FolderType: "folder"}}, Messages: []CachedMessage{{Message: domain.Message{ID: "m", AccountID: "a", RemoteID: "INBOX:1:1", Subject: "original"}, FolderIDs: []string{"f"}}}, ProviderCheckpoints: []ProviderCheckpoint{{Provider: "imap", Scope: "INBOX", Data: []byte(`{"uid":1}`)}}, ProviderMappings: []ProviderMapping{{Provider: "imap", Scope: "INBOX:1", RemoteID: "1", MessageID: "m"}}}
	if err := db.CommitSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	execute(t, db.write, `CREATE TRIGGER fail_checkpoint BEFORE UPDATE ON provider_checkpoints BEGIN SELECT RAISE(ABORT,'fault'); END`)
	snapshot.Messages[0].Message.Subject = "must roll back"
	snapshot.ProviderCheckpoints[0].Data = []byte(`{"uid":2}`)
	if err := db.CommitSnapshot(t.Context(), snapshot); err == nil {
		t.Fatal("expected injected transaction failure")
	}
	var subject string
	if err := db.read.QueryRow(`SELECT subject FROM messages WHERE id='m'`).Scan(&subject); err != nil || subject != "original" {
		t.Fatal(subject, err)
	}
	execute(t, db.write, `DROP TRIGGER fail_checkpoint`)
	bad := Snapshot{AccountID: "b", ProviderMappings: []ProviderMapping{{Provider: "imap", Scope: "INBOX:1", RemoteID: "1", MessageID: "m"}}, ProviderCheckpoints: []ProviderCheckpoint{{Provider: "imap", Scope: "INBOX", Data: []byte("bad")}}}
	if err := db.CommitSnapshot(t.Context(), bad); err == nil {
		t.Fatal("cross-account mapping accepted")
	}
	if _, err := db.ProviderCheckpoint(t.Context(), "b", "imap", "INBOX"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	data, err := db.ProviderCheckpoint(t.Context(), "a", "imap", "INBOX")
	if err != nil || !bytes.Equal(data, []byte(`{"uid":1}`)) {
		t.Fatal(string(data), err)
	}
	mappings, err := db.ProviderMappingsPage(t.Context(), "a", "imap", "INBOX:1", "", 1)
	if err != nil || len(mappings) != 1 || mappings[0].MessageID != "m" {
		t.Fatal(mappings, err)
	}
	ids, err := db.RemoteIDsPage(t.Context(), "a", "INBOX:", "", 1)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if err := db.DeleteAccount(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ProviderCheckpoint(t.Context(), "a", "imap", "INBOX"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("checkpoint survived account deletion", err)
	}
}

func TestProtocolAccountCreateDoesNotOverwriteAndCASDoesNotResurrect(t *testing.T) {
	db := openStore(t)
	a := domain.Account{ID: "a", Email: "User@example.invalid", Provider: "pop3", DisplayName: "working"}
	if err := db.CreateProtocolAccount(t.Context(), a, []byte("old")); err != nil {
		t.Fatal(err)
	}
	duplicate := a
	duplicate.ID = "other"
	duplicate.Email = "user@EXAMPLE.invalid"
	if err := db.CreateProtocolAccount(t.Context(), duplicate, []byte("replace")); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	if err := db.CreateProtocolAccount(t.Context(), a, []byte("replace")); err == nil {
		t.Fatal("upsert overwrote account")
	}
	a.DisplayName = "new"
	if ok, err := db.ReplaceProtocolAccount(t.Context(), a, []byte("stale"), []byte("new")); err != nil || ok {
		t.Fatal(ok, err)
	}
	saved, err := db.Account(t.Context(), a.ID)
	if err != nil || saved.DisplayName != "working" {
		t.Fatal(saved, err)
	}
	if ok, err := db.ReplaceProtocolAccount(t.Context(), a, []byte("old"), []byte("new")); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := db.DeleteAccount(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ReplaceProtocolAccount(t.Context(), a, []byte("new"), []byte("resurrect")); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestV32MigrationRollsBackBeforeVersionAndPreservesLegacy(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 31); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at,auth_data) VALUES('a','same@example.invalid','imap',1,1,x'01'),('b','SAME@example.invalid','imap',1,1,x'02')`)
	if err := migrate(t.Context(), db, 32); err == nil {
		t.Fatal("expected duplicate identity migration failure")
	}
	if v := integer(t, db, "PRAGMA user_version"); v != 31 {
		t.Fatal(v)
	}
	if n := integer(t, db, `SELECT count(*) FROM sqlite_master WHERE name='provider_checkpoints'`); n != 0 {
		t.Fatal("partial DDL escaped rollback")
	}
	checkLegacySchema(t, db)
}
