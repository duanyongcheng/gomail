package store

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func TestGmailSettingsAtomicAndNarrowWithSignature(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "a@example.invalid", Provider: "gmail", DisplayName: "Original", CreatedAt: 7}, []byte("original auth")); err != nil {
		t.Fatal(err)
	}
	execute(t, db.write, `UPDATE accounts SET sync_state='{"last_sync_cursor":"opaque","gmail_auth_status":"reauth_required"}',oauth_subject='subject',sort_order=9 WHERE id='a'`)
	color, label := "#abcdef", "Work"
	settings := AccountSettings{AccountID: "a", Email: "A@example.invalid", DisplayName: "New sender", Color: &color, Label: &label, Signature: []byte("encrypted signature")}
	execute(t, db.write, `CREATE TRIGGER reject_signature BEFORE INSERT ON email_signatures BEGIN SELECT RAISE(ABORT,'fixture failure'); END`)
	if err := db.UpdateGmailSettings(ctx, settings); err == nil {
		t.Fatal("expected signature fault")
	}
	a, _ := db.Account(ctx, "a")
	if a.DisplayName != "Original" || a.AccountLabel != nil {
		t.Fatal("partial settings commit", a)
	}
	execute(t, db.write, `DROP TRIGGER reject_signature`)
	if err := db.UpdateGmailSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	a, err := db.Account(ctx, "a")
	if err != nil || a.DisplayName != "New sender" || *a.Color != color || *a.AccountLabel != label || a.Email != "a@example.invalid" || a.SortOrder != 9 || a.CreatedAt != 7 || *a.OAuthSubject != "subject" {
		t.Fatal(a, err)
	}
	var auth []byte
	var state string
	if err := db.read.QueryRowContext(ctx, "SELECT auth_data,sync_state FROM accounts WHERE id='a'").Scan(&auth, &state); err != nil || string(auth) != "original auth" || state != `{"last_sync_cursor":"opaque","gmail_auth_status":"reauth_required"}` {
		t.Fatal("unrelated state overwritten", err)
	}
	settings.Email = "other@example.invalid"
	settings.DisplayName = "Wrong mailbox"
	if err := db.UpdateGmailSettings(ctx, settings); err == nil {
		t.Fatal("accepted mailbox reassignment")
	}
	settings.Email = a.Email
	settings.Signature = nil
	settings.Color, settings.Label = nil, nil
	if err := db.UpdateGmailSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	data, err := db.EmailSignature(ctx, "a")
	if err != nil || !bytes.Equal(data, []byte("encrypted signature")) {
		t.Fatal("omitted signature was cleared", err)
	}
	if err := db.DeleteAccount(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if integer(t, db.read, "SELECT count(*) FROM email_signatures") != 0 {
		t.Fatal("signature did not cascade")
	}
	if err := db.UpdateGmailSettings(ctx, settings); err == nil {
		t.Fatal("removed account recreated")
	}
}

func TestSignatureMigrationPreservesWinnerAndEncryptionDetection(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(ctx, domain.Account{ID: id, Email: id + "@example.invalid", Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"encrypted new signature", "encrypted explicit empty"} {
		if _, err := db.SaveEmailSignature(ctx, "a", []byte(value), false); err != nil {
			t.Fatal(err)
		}
		stored, err := db.SaveEmailSignature(ctx, "a", []byte("stale legacy"), true)
		if err != nil || string(stored) != value {
			t.Fatal("legacy overwrote saved state", string(stored), err)
		}
	}
	if data, err := db.EmailSignature(ctx, "b"); err != nil || data != nil {
		t.Fatal("account isolation", data, err)
	}
	if has, err := db.HasEncryptedData(ctx); err != nil || !has {
		t.Fatal("signature not counted as encrypted data", err)
	}
	if _, err := db.SaveEmailSignature(ctx, "missing", []byte("data"), true); err == nil {
		t.Fatal("migration created missing account")
	}
	if _, err := db.EmailSignature(ctx, "missing"); err != sql.ErrNoRows {
		t.Fatal("missing account signature read", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.SaveEmailSignature(cancelled, "a", []byte("cancelled"), false); err == nil {
		t.Fatal("cancelled save succeeded")
	}
}

func TestV29SignatureMigrationPreservesLegacyAndAccountData(t *testing.T) {
	raw := openRaw(t)
	if err := migrate(t.Context(), raw, 28); err != nil {
		t.Fatal(err)
	}
	execute(t, raw, `INSERT INTO accounts(id,email,display_name,provider,auth_data,created_at,updated_at) VALUES('a','a@example.invalid','Sender','gmail',X'0102',1,1)`)
	if err := migrate(t.Context(), raw, 29); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, raw)
	if integer(t, raw, "SELECT count(*) FROM accounts WHERE id='a' AND hex(auth_data)='0102'") != 1 || integer(t, raw, "SELECT count(*) FROM email_signatures") != 0 {
		t.Fatal("upgrade changed existing account")
	}
}
