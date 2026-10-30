package store

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func cloudOutboxStoreFixture(t *testing.T) (*DB, OutboxRecord) {
	t.Helper()
	db := outboxFixture(t)
	ctx := t.Context()
	draft := EncryptedDraft{ID: "local", AccountID: "a", RequestKey: "draft-key", Data: []byte("encrypted-local")}
	if err := db.SaveComposeDraft(ctx, draft, nil); err != nil {
		t.Fatal(err)
	}
	copy := GmailDraftCopy{DraftID: "local", AccountID: "a", Revision: 1, State: "pending", Data: []byte("encrypted-identity")}
	if err := db.BeginGmailDraftCopy(ctx, copy); err != nil {
		t.Fatal(err)
	}
	copy.State, copy.RemoteID, copy.RemoteMessageID = "synced", "remote", "remote-message"
	if err := db.SettleGmailDraftCopy(ctx, copy); err != nil {
		t.Fatal(err)
	}
	return db, OutboxRecord{
		ID: "send", AccountID: "a", RequestKey: "send-key", Data: []byte("encrypted-message"), Summary: []byte("encrypted-summary"),
		CloudDraft: &OutboxDraftReservation{Copy: copy, LocalRevision: 1},
	}
}

func TestCloudOutboxReservationIsAtomicAndSingleOwner(t *testing.T) {
	db, record := cloudOutboxStoreFixture(t)
	ctx := t.Context()
	stale := *record.CloudDraft
	stale.LocalRevision++
	record.CloudDraft = &stale
	if _, err := db.QueueOutbox(ctx, record); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("stale snapshot queued", err)
	}
	if _, err := db.Outbox(ctx, "a", record.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failed reservation kept outbox", err)
	}
	record.CloudDraft.LocalRevision--
	execute(t, db.write, `CREATE TRIGGER fail_reserve BEFORE UPDATE OF state ON gmail_draft_copies WHEN NEW.state='sending' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := db.QueueOutbox(ctx, record); err == nil {
		t.Fatal("reservation failure ignored")
	}
	if _, err := db.Outbox(ctx, "a", record.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("reservation SQL failure kept outbox", err)
	}
	execute(t, db.write, "DROP TRIGGER fail_reserve")
	if _, err := db.QueueOutbox(ctx, record); err != nil {
		t.Fatal(err)
	}
	copy, err := db.GmailDraftCopy(ctx, "a", "local")
	if err != nil || copy.State != "sending" || copy.OutboxID != record.ID {
		t.Fatal(copy, err)
	}
	second := record
	second.ID, second.RequestKey = "second", "different-key"
	if _, err := db.QueueOutbox(ctx, second); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("second owner accepted", err)
	}
	if id, err := db.QueueOutbox(ctx, record); err != nil || id != record.ID {
		t.Fatal("idempotent queue changed reservation", id, err)
	}
	if err := db.OutboxAction(ctx, "b", record.ID, "cancel"); err == nil {
		t.Fatal("cross-account cancel accepted")
	}
	if err := db.OutboxAction(ctx, "a", record.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	copy, _ = db.GmailDraftCopy(ctx, "a", "local")
	if copy.State != "synced" || copy.OutboxID != "" {
		t.Fatal("cancel did not release safe reservation", copy)
	}
	if _, err := db.QueueOutbox(ctx, second); err != nil {
		t.Fatal("safe cancellation blocked next explicit send", err)
	}
}

func TestCloudOutboxSendBarrierAndAtomicReceipt(t *testing.T) {
	db, record := cloudOutboxStoreFixture(t)
	ctx := t.Context()
	if _, err := db.QueueOutbox(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimOutbox(ctx, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginCloudOutboxSend(ctx, "b", record.ID, "local", "remote", "remote-message"); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("wrong-account barrier passed", err)
	}
	if err := db.BeginCloudOutboxSend(ctx, "a", record.ID, "local", "remote", "changed-message"); !errors.Is(err, ErrDraftConflict) {
		t.Fatal("stale remote barrier passed", err)
	}
	if err := db.DeleteComposeDraft(ctx, "a", "local"); err != nil {
		t.Fatal("post-queue cleanup blocked", err)
	}
	retained, err := db.ComposeDraft(ctx, "a", "local")
	if err != nil || retained.Deleted || !bytes.Equal(retained.Data, []byte("encrypted-local")) {
		t.Fatal("post-queue cleanup deleted the latest local snapshot", err)
	}
	if err := db.BeginCloudOutboxSend(ctx, "a", record.ID, "local", "remote", "remote-message"); err != nil {
		t.Fatal("cleanup lost reservation", err)
	}
	execute(t, db.write, `CREATE TRIGGER fail_cloud_receipt BEFORE UPDATE OF state ON gmail_draft_copies WHEN NEW.state='sent' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.SettleOutbox(ctx, record.ID, "sent", "", "sent-id", false); err == nil {
		t.Fatal("partial receipt accepted")
	}
	row, _ := db.Outbox(ctx, "a", record.ID)
	copy, _ := db.GmailDraftCopy(ctx, "a", "local")
	if row.Status != "in_progress" || copy.State != "sending" {
		t.Fatal("receipt was not atomic", row.Status, copy.State)
	}
	execute(t, db.write, "DROP TRIGGER fail_cloud_receipt")
	if err := db.SettleOutbox(ctx, record.ID, "sent", "", "sent-id", false); err != nil {
		t.Fatal(err)
	}
	row, _ = db.Outbox(ctx, "a", record.ID)
	copy, _ = db.GmailDraftCopy(ctx, "a", "local")
	if row.Status != "sent" || copy.State != "sent" {
		t.Fatal(row.Status, copy.State)
	}
	retained, err = db.ComposeDraft(ctx, "a", "local")
	if err != nil || !retained.Deleted || retained.Data != nil {
		t.Fatal("receipt did not tombstone retained draft", err)
	}
}

func TestCloudOutboxUnknownKeepsIdentityUntilConfirmed(t *testing.T) {
	db, record := cloudOutboxStoreFixture(t)
	ctx := t.Context()
	if _, err := db.QueueOutbox(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimOutbox(ctx, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginCloudOutboxSend(ctx, "a", record.ID, "local", "remote", "remote-message"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.OutboxAction(ctx, "a", record.ID, "dismiss"); err == nil {
		t.Fatal("dismiss removed protected cloud send identity")
	}
	if _, err := db.ClaimOutbox(ctx, time.Now().Add(24*time.Hour).Unix()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unknown send reclaimed", err)
	}
	row, _ := db.Outbox(ctx, "a", record.ID)
	copy, _ := db.GmailDraftCopy(ctx, "a", "local")
	if row.Status != "outcome_unknown" || len(row.Data) == 0 || copy.State != "sending" {
		t.Fatal(row.Status, copy.State)
	}
	if err := db.ConfirmOutboxSent(ctx, "a", record.ID, "confirmed-id"); err != nil {
		t.Fatal(err)
	}
	copy, _ = db.GmailDraftCopy(ctx, "a", "local")
	if copy.State != "sent" {
		t.Fatal(copy.State)
	}
	if err := db.OutboxAction(ctx, "a", record.ID, "dismiss"); err != nil {
		t.Fatal(err)
	}
}
