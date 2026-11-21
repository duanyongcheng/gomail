package store

import (
	"context"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func trustFixture(t *testing.T) *DB {
	t.Helper()
	db := openStore(t)
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id, Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func TestTrustedSendersIsolationNormalizationAndReopen(t *testing.T) {
	db := trustFixture(t)
	ctx := t.Context()
	if err := db.TrustSender(ctx, "a", " Sender@Example.invalid ", "all"); err != nil {
		t.Fatal(err)
	}
	if err := db.TrustSender(ctx, "b", "sender@example.invalid", "images"); err != nil {
		t.Fatal(err)
	}
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for account, want := range map[string]string{"a": "all", "b": "images", "other": ""} {
		got, err := db.SenderTrust(ctx, account, "SENDER@example.invalid")
		if err != nil || got != want {
			t.Fatal(account, got, err)
		}
	}
	list, err := db.TrustedSenders(ctx, "a")
	if err != nil || len(list) != 1 || list[0].Email != "sender@example.invalid" || list[0].CreatedAt <= 0 {
		t.Fatal(list, err)
	}
	if err := db.RemoveTrustedSender(ctx, "a", "SENDER@EXAMPLE.INVALID"); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.SenderTrust(ctx, "a", "sender@example.invalid"); got != "" {
		t.Fatal("revoke failed")
	}
	if got, _ := db.SenderTrust(ctx, "b", "sender@example.invalid"); got != "images" {
		t.Fatal("cross-account revoke")
	}
	if err := db.DeleteAccount(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM trusted_senders") != 0 {
		t.Fatal("account cascade retained trust")
	}
}
func TestLegacyTrustVariantsUseStrictestLevelAndAtomicReplacement(t *testing.T) {
	db := trustFixture(t)
	ctx := t.Context()
	execute(t, db.write, `INSERT INTO trusted_senders VALUES('a','Sender@example.invalid','all',1),('a','sender@example.invalid','images',2)`)
	got, err := db.SenderTrust(ctx, "a", "Sender@example.invalid")
	if err != nil || got != "images" {
		t.Fatal(got, err)
	}
	list, err := db.TrustedSenders(ctx, "a")
	if err != nil || len(list) != 1 || list[0].TrustType != "images" || list[0].CreatedAt != 1 {
		t.Fatal(list, err)
	}
	execute(t, db.write, `CREATE TRIGGER fail_trust BEFORE INSERT ON trusted_senders BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.TrustSender(ctx, "a", "sender@example.invalid", "all"); err == nil {
		t.Fatal("write failure ignored")
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM trusted_senders") != 2 {
		t.Fatal("delete before failed insert not rolled back")
	}
	execute(t, db.write, "DROP TRIGGER fail_trust")
	if err := db.TrustSender(ctx, "a", "sender@example.invalid", "all"); err != nil {
		t.Fatal(err)
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM trusted_senders") != 1 {
		t.Fatal("case duplicates retained")
	}
	if got, _ := db.SenderTrust(ctx, "a", "Sender@example.invalid"); got != "all" {
		t.Fatal(got)
	}
	checkLegacySchema(t, db.write)
}
func TestTrustValidationAndCancellation(t *testing.T) {
	db := trustFixture(t)
	for _, args := range [][3]string{{"a", "sender@example.invalid", "unknown"}, {"missing", "sender@example.invalid", "all"}, {"", "sender@example.invalid", "images"}, {"a", "*.example.invalid", "all"}, {"a", "Name <sender@example.invalid>", "all"}} {
		if err := db.TrustSender(t.Context(), args[0], args[1], args[2]); err == nil {
			t.Fatal("invalid trust accepted", args)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := db.TrustSender(ctx, "a", "sender@example.invalid", "all"); err == nil {
		t.Fatal("cancellation ignored")
	}
}
