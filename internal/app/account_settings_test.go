package app

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duanyongcheng/gomail/internal/mailmime"
	"github.com/duanyongcheng/gomail/internal/store"
)

func TestAccountSettingsEncryptedRestartAndOutgoingName(t *testing.T) {
	compose, ring := composeFixture(t)
	s := &AccountService{Runtime: compose.Runtime}
	ctx := t.Context()
	color, label, sig := "#Ab1234", "Local work label", "Private regards\n中文签名"
	if _, err := s.Update(ctx, UpdateAccountRequest{AccountID: "a", Email: "a@example.invalid", DisplayName: "Saved sender", AccountColor: &color, AccountLabel: &label, Signature: &sig}); err != nil {
		t.Fatal(err)
	}
	data, err := s.Runtime.DB.EmailSignature(ctx, "a")
	if err != nil || bytes.Contains(data, []byte(sig)) || !bytes.HasPrefix(data, []byte("GOMAIL")) {
		t.Fatal("plaintext signature", err)
	}
	if other, err := s.Signature(ctx, AccountRequest{AccountID: "b"}); err != nil || other != "" {
		t.Fatal("cross-account signature", other, err)
	}
	path := s.Runtime.DB.Path()
	if err := s.Runtime.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := New(ctx, db, ring, nil)
	defer r.Scheduler.Close()
	s = &AccountService{Runtime: r}
	if got, err := s.Signature(ctx, AccountRequest{AccountID: "a"}); err != nil || got != sig {
		t.Fatal("signature not restored", got, err)
	}
	a, err := db.Account(ctx, "a")
	if err != nil || a.DisplayName != "Saved sender" || *a.AccountLabel != label || *a.Color != color {
		t.Fatal(a, err)
	}
	// Signature insertion belongs to the editor. The backend freezes the sender
	// name and composed body in MIME; later preference changes cannot rewrite it.
	outbox := &OutboxService{Runtime: r}
	if _, err := outbox.Queue(ctx, SendRequest{AccountID: "a", ClientKey: "settings-send", To: []string{"to@example.invalid"}, Subject: "Saved preferences", BodyText: "Body\n" + sig}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.OutboxList(ctx, "a")
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	record, err := db.Outbox(ctx, "a", rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := r.decodeOutbox(ctx, record)
	parsed, parseErr := mailmime.Parse(payload.Raw)
	if err != nil || parseErr != nil || parsed.From.Name == nil || *parsed.From.Name != "Saved sender" || !strings.Contains(strings.ReplaceAll(parsed.BodyText, "\r\n", "\n"), sig) {
		t.Fatal("saved settings missing from MIME", parsed, err, parseErr)
	}
	if _, err := s.Update(ctx, UpdateAccountRequest{AccountID: "a", Email: a.Email, DisplayName: "Later sender"}); err != nil {
		t.Fatal(err)
	}
	after, _ := db.Outbox(ctx, "a", rows[0].ID)
	if !bytes.Equal(after.Data, record.Data) {
		t.Fatal("queued MIME was rewritten")
	}
}

func TestAccountSettingsValidationAndLocalOnlySignature(t *testing.T) {
	compose, _ := composeFixture(t)
	s := &AccountService{Runtime: compose.Runtime}
	ctx := t.Context()
	sig := "Keep original signature"
	if _, err := s.SetSignature(ctx, SignatureRequest{AccountID: "a", Signature: sig}); err != nil {
		t.Fatal(err)
	}
	bad := "#bad"
	proxy := "127.0.0.1"
	for _, req := range []UpdateAccountRequest{
		{AccountID: "a", Email: "other@example.invalid", DisplayName: "Mismatch"},
		{AccountID: "a", Email: "a@example.invalid", DisplayName: "Injected\r\nBcc: x"},
		{AccountID: "a", Email: "a@example.invalid", AccountColor: &bad},
		{AccountID: "a", Email: "a@example.invalid", ProxyHost: &proxy},
		{AccountID: "missing", Email: "a@example.invalid"},
	} {
		if _, err := s.Update(ctx, req); err == nil {
			t.Fatal("accepted invalid/unsupported update")
		}
	}
	if got, err := s.Signature(ctx, AccountRequest{AccountID: "a"}); err != nil || got != sig {
		t.Fatal("failed update lost signature", got, err)
	}
	for _, value := range []string{strings.Repeat("s", maxSignatureBytes+1), "bad\x00text", string([]byte{0xff})} {
		if _, err := s.SetSignature(ctx, SignatureRequest{AccountID: "a", Signature: value}); err == nil {
			t.Fatal("accepted invalid signature")
		}
	}
	if _, err := s.SetSignature(ctx, SignatureRequest{AccountID: "a", Signature: ""}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.MigrateSignature(ctx, SignatureRequest{AccountID: "a", Signature: "stale legacy"}); err != nil || got != "" {
		t.Fatal("migration restored explicitly cleared signature", got, err)
	}
	if _, err := s.SetSignature(ctx, SignatureRequest{AccountID: "b", Signature: "Other account"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Signature(ctx, AccountRequest{AccountID: "b"}); err != nil || got != "Other account" {
		t.Fatal(got, err)
	}
}

func TestSignatureMissingKeyTamperAndAccountBinding(t *testing.T) {
	compose, ring := composeFixture(t)
	s := &AccountService{Runtime: compose.Runtime}
	ctx := t.Context()
	if _, err := s.SetSignature(ctx, SignatureRequest{AccountID: "a", Signature: "Only encrypted record"}); err != nil {
		t.Fatal(err)
	}
	original, _ := s.Runtime.DB.EmailSignature(ctx, "a")
	// Copying ciphertext to another account must not decrypt under that account.
	if _, err := s.Runtime.DB.SaveEmailSignature(ctx, "b", original, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Signature(ctx, AccountRequest{AccountID: "b"}); err == nil {
		t.Fatal("AAD binding bypassed")
	}
	ring.key = nil
	r := New(ctx, s.Runtime.DB, ring, nil)
	defer r.Scheduler.Close()
	locked := &AccountService{Runtime: r}
	if _, err := locked.Signature(ctx, AccountRequest{AccountID: "a"}); err == nil {
		t.Fatal("missing key accepted")
	}
	sig := "Must not overwrite"
	if _, err := locked.Update(ctx, UpdateAccountRequest{AccountID: "a", Email: "a@example.invalid", DisplayName: "Must not save", Signature: &sig}); err == nil {
		t.Fatal("missing key update succeeded")
	}
	preserved, _ := r.DB.EmailSignature(ctx, "a")
	if ring.writes != 0 || !bytes.Equal(preserved, original) {
		t.Fatal("missing DEK replaced or ciphertext changed")
	}
	// Corrupt an authenticated record; reading must fail without clearing it.
	tampered := append([]byte(nil), original...)
	tampered[len(tampered)-1] ^= 1
	if _, err := s.Runtime.DB.SaveEmailSignature(ctx, "a", tampered, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Signature(ctx, AccountRequest{AccountID: "a"}); err == nil {
		t.Fatal("tampered signature accepted")
	}
}

func TestAccountSettingsStorageFaultRollsBackAndEmitsNoSuccess(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	path := filepath.Clean(r.DB.Path())
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE TRIGGER reject_account_signature BEFORE INSERT ON email_signatures BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	events := 0
	r.OnAccountChanged = func(AccountRequest) { events++ }
	sig := "Private signature"
	s := &AccountService{Runtime: r}
	if _, err := s.Update(t.Context(), UpdateAccountRequest{AccountID: "a", Email: "a@example.invalid", DisplayName: "No partial save", Signature: &sig}); err == nil {
		t.Fatal("injected failure accepted")
	}
	a, _ := r.DB.Account(t.Context(), "a")
	if a.DisplayName != "" || events != 0 {
		t.Fatal("partial save or false success event", a, events)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Update(cancelled, UpdateAccountRequest{AccountID: "a", Email: a.Email}); err == nil {
		t.Fatal("cancelled update accepted")
	}
}
