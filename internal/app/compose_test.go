package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/store"
)

func composeFixture(t *testing.T) (*ComposeService, *testKeyring) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id + "@example.invalid", Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	ring := &testKeyring{key: bytes.Repeat([]byte{9}, 32)}
	r := New(t.Context(), db, ring, nil)
	t.Cleanup(r.Scheduler.Close)
	return &ComposeService{Runtime: r}, ring
}
func draftFixture() SaveDraftRequest {
	return SaveDraftRequest{AccountID: "a", ClientKey: "stable-key", Subject: "private draft subject", To: []string{"partial-recipient"}, CC: []string{"cc@example.invalid"}, BCC: []string{"secret@example.invalid"}, BodyText: "private draft body", BodyHTML: "<p>private draft body</p>", RawSource: "**markdown source**", EditorMode: "markdown", InReplyTo: "original@example.invalid", QuotedReplyHTML: "<blockquote>original quote</blockquote>"}
}
func TestComposeEncryptedRoundTripRestartAndIdempotentSave(t *testing.T) {
	s, ring := composeFixture(t)
	ctx := t.Context()
	path, err := s.Stage(ctx, StageComposeRequest{Filename: "secret-name.txt", Bytes: []int{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	r := draftFixture()
	r.AttachmentPaths = []string{path}
	id, err := s.Save(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if id2, err := s.Save(ctx, r); err != nil || id2 != id {
		t.Fatal("idempotence", id2, err)
	}
	if _, err := s.Cleanup(ctx, CleanupComposeRequest{Path: path}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Runtime.DB.ComposeDraft(ctx, "a", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{r.Subject, r.BodyText, r.BCC[0], "secret-name.txt"} {
		if bytes.Contains(stored.Data, []byte(text)) {
			t.Fatal("plaintext draft persisted")
		}
	}
	aid, _ := composeID(path)
	a, err := s.Runtime.DB.ComposeAttachment(ctx, aid)
	if err != nil {
		t.Fatal("referenced attachment removed", err)
	}
	if bytes.Contains(a.Data, []byte("secret-name.txt")) {
		t.Fatal("plaintext attachment metadata")
	}
	databasePath := s.Runtime.DB.Path()
	if err := s.Runtime.DB.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := store.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	runtime := New(ctx, other, ring, nil)
	defer runtime.Scheduler.Close()
	reopened := &ComposeService{Runtime: runtime}
	got, err := reopened.Get(ctx, DraftRequest{AccountID: "a", DraftID: id})
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != r.Subject || got.BodyText != r.BodyText || got.RawSource != r.RawSource || got.InReplyTo != r.InReplyTo || got.QuotedReplyHTML != r.QuotedReplyHTML || len(got.Attachments) != 1 || got.Attachments[0].Name != "secret-name.txt" || len(got.BCC) != 1 {
		t.Fatal(got)
	}
	list, err := reopened.List(ctx, AccountRequest{AccountID: "a"})
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	r.ExistingDraftID = id
	r.Subject = "updated"
	if _, err := reopened.Save(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Delete(ctx, DraftRequest{AccountID: "a", DraftID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := other.ComposeAttachment(ctx, aid); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("last reference not released", err)
	}
	if _, err := reopened.Save(ctx, r); !errors.Is(err, store.ErrDraftConflict) {
		t.Fatal("late save resurrected draft", err)
	}
	r.ExistingDraftID = ""
	if _, err := reopened.Save(ctx, r); !errors.Is(err, store.ErrDraftConflict) {
		t.Fatal("request-key retry resurrected draft", err)
	}
}
func TestComposeOwnershipFailuresAndMissingKeyNeverOverwrite(t *testing.T) {
	s, ring := composeFixture(t)
	r := draftFixture()
	id, err := s.Save(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(t.Context(), DraftRequest{AccountID: "b", DraftID: id}); err == nil {
		t.Fatal("cross-account get")
	}
	if _, err := s.Delete(t.Context(), DraftRequest{AccountID: "b", DraftID: id}); err == nil {
		t.Fatal("cross-account delete")
	}
	r.AccountID = "b"
	r.ExistingDraftID = id
	if _, err := s.Save(t.Context(), r); err == nil {
		t.Fatal("cross-account update")
	}
	missing := &testKeyring{err: crypto.ErrKeyNotFound}
	runtime := New(t.Context(), s.Runtime.DB, missing, nil)
	defer runtime.Scheduler.Close()
	if _, err := (&ComposeService{Runtime: runtime}).Save(t.Context(), draftFixture()); err == nil || missing.writes != 0 {
		t.Fatal("missing key replaced", err, missing.writes)
	}
	ring.key = bytes.Repeat([]byte{8}, 32)
	runtime2 := New(t.Context(), s.Runtime.DB, ring, nil)
	defer runtime2.Scheduler.Close()
	if _, err := (&ComposeService{Runtime: runtime2}).Save(t.Context(), draftFixture()); err == nil {
		t.Fatal("wrong key overwrote draft")
	}
	got, err := s.Get(t.Context(), DraftRequest{AccountID: "a", DraftID: id})
	if err != nil || got.Subject != draftFixture().Subject {
		t.Fatal(got, err)
	}
}
func TestComposeValidationAndCancelledRequest(t *testing.T) {
	s, _ := composeFixture(t)
	for _, p := range []string{"/etc/passwd", "../profile.sqlite", "compose-attachment:../../file"} {
		if _, err := s.Cleanup(t.Context(), CleanupComposeRequest{Path: p}); err == nil {
			t.Fatal("arbitrary path accepted")
		}
	}
	if _, err := s.Stage(t.Context(), StageComposeRequest{Filename: "x", Bytes: []int{256}}); err == nil {
		t.Fatal("invalid byte accepted")
	}
	r := draftFixture()
	r.BodyText = strings.Repeat("x", maxDraftBodyBytes+1)
	if _, err := s.Save(t.Context(), r); err == nil {
		t.Fatal("body limit ignored")
	}
	r = draftFixture()
	r.AttachmentPaths = []string{"/etc/passwd"}
	if _, err := s.Save(t.Context(), r); err == nil {
		t.Fatal("file path read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Save(ctx, draftFixture()); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestTamperedAttachmentCannotBeRestoredOrSilentlyResaved(t *testing.T) {
	s, _ := composeFixture(t)
	path, err := s.Stage(t.Context(), StageComposeRequest{Filename: "private.txt", Bytes: []int{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	r := draftFixture()
	r.AttachmentPaths = []string{path}
	id, err := s.Save(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", s.Runtime.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE compose_attachments SET data=X'010203'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(t.Context(), DraftRequest{AccountID: "a", DraftID: id}); err == nil {
		t.Fatal("tampered attachment restored")
	}
	r.ExistingDraftID = id
	if _, err := s.Save(t.Context(), r); err == nil {
		t.Fatal("tampered attachment resaved")
	}
	stored, err := s.Runtime.DB.ComposeDraft(t.Context(), "a", id)
	if err != nil || stored.Revision != 1 {
		t.Fatal(stored.Revision, err)
	}
}

func TestComposeAttachmentRetainedAcrossTwoDrafts(t *testing.T) {
	s, _ := composeFixture(t)
	ctx := t.Context()
	p, err := s.Stage(ctx, StageComposeRequest{Filename: "shared.txt", Bytes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	r := draftFixture()
	r.AttachmentPaths = []string{p}
	first, err := s.Save(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	r.AccountID = "b"
	second, err := s.Save(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cleanup(ctx, CleanupComposeRequest{Path: p}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete(ctx, DraftRequest{AccountID: "a", DraftID: first}); err != nil {
		t.Fatal(err)
	}
	id, _ := composeID(p)
	if _, err := s.Runtime.DB.ComposeAttachment(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, DraftRequest{AccountID: "b", DraftID: second}); err != nil {
		t.Fatal(err)
	}
	if err := s.Runtime.DB.DeleteAccount(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Runtime.DB.ComposeAttachment(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("account cascade leaked released attachment", err)
	}
}
