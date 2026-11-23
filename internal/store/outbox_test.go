package store

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
)

func outboxFixture(t *testing.T) *DB {
	t.Helper()
	db := openStore(t)
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id, Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func queueOutbox(t *testing.T, db *DB, id string) {
	t.Helper()
	if _, err := db.QueueOutbox(t.Context(), OutboxRecord{ID: id, AccountID: "a", RequestKey: id, Data: []byte("cipher"), Summary: []byte("summary")}); err != nil {
		t.Fatal(err)
	}
}
func TestOutboxIdempotencyClaimRecoveryAndActions(t *testing.T) {
	db := outboxFixture(t)
	ctx := t.Context()
	queueOutbox(t, db, "first")
	queueOutbox(t, db, "second")
	if id, err := db.QueueOutbox(ctx, OutboxRecord{ID: "different", AccountID: "a", RequestKey: "first", Data: []byte("overwrite")}); err != nil || id != "first" {
		t.Fatal(id, err)
	}
	first, err := db.ClaimOutbox(ctx, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BeginOutboxSend(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimOutbox(ctx, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	unknown, _ := db.Outbox(ctx, "a", first.ID)
	prepared, _ := db.Outbox(ctx, "a", second.ID)
	if unknown.Status != "outcome_unknown" || prepared.Status != "queued" {
		t.Fatal(unknown.Status, prepared.Status)
	}
	if err := db.OutboxAction(ctx, "a", first.ID, "retry"); err == nil {
		t.Fatal("unknown retry allowed")
	}
	if err := db.OutboxAction(ctx, "b", second.ID, "cancel"); err == nil {
		t.Fatal("cross-account action")
	}
	if err := db.OutboxAction(ctx, "a", second.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimOutbox(ctx, time.Now().Add(time.Hour).Unix()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unknown/cancelled reclaimed", err)
	}
	if err := db.ConfirmOutboxSent(ctx, "a", first.ID, "remote"); err != nil {
		t.Fatal(err)
	}
	if err := db.OutboxAction(ctx, "a", first.ID, "dismiss"); err != nil {
		t.Fatal(err)
	}
	row, _ := db.Outbox(ctx, "a", first.ID)
	if row.Data != nil || row.Summary != nil || row.Status != "dismissed" {
		t.Fatal(row)
	}
}
func TestOutboxBackoffAndAtomicStartFailure(t *testing.T) {
	db := outboxFixture(t)
	queueOutbox(t, db, "one")
	ctx := t.Context()
	for i := 0; i < MaxOutboxAttempts; i++ {
		row, err := db.ClaimOutbox(ctx, time.Now().Add(time.Hour).Unix())
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SettleOutbox(ctx, row.ID, "failed", "offline", "", true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ClaimOutbox(ctx, time.Now().Add(24*time.Hour).Unix()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("retry budget ignored", err)
	}
	if err := db.OutboxAction(ctx, "a", "one", "retry"); err != nil {
		t.Fatal(err)
	}
	row, err := db.ClaimOutbox(ctx, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	execute(t, db.write, `CREATE TRIGGER fail_start BEFORE UPDATE OF status ON outbox WHEN NEW.status='in_progress' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := db.BeginOutboxSend(ctx, row.ID); err == nil {
		t.Fatal("start failure ignored")
	}
	row, _ = db.Outbox(ctx, "a", row.ID)
	if row.Status != "preparing" {
		t.Fatal(row.Status)
	}
}
func TestOutboxConcurrentClaimAndV26Migration(t *testing.T) {
	db := outboxFixture(t)
	queueOutbox(t, db, "one")
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := 0
	for range 4 {
		wg.Go(func() {
			_, err := db.ClaimOutbox(t.Context(), time.Now().Unix())
			if err == nil {
				mu.Lock()
				claims++
				mu.Unlock()
			} else if !errors.Is(err, sql.ErrNoRows) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if claims != 1 {
		t.Fatal(claims)
	}
	raw := openRaw(t)
	if err := migrate(t.Context(), raw, 25); err != nil {
		t.Fatal(err)
	}
	if err := migrate(t.Context(), raw, 26); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, raw)
}
