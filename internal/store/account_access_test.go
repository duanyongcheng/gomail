package store

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/domain"
)

func TestAccountAccessStateAndCredentialReplacementPreserveSyncState(t *testing.T) {
	db := openStore(t)
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id + "@example.invalid", Provider: "gmail"}, []byte("old")); err != nil {
			t.Fatal(err)
		}
	}
	execute(t, db.write, `UPDATE accounts SET sync_state='{"gmail_checkpoint":{"phase":"messages","page_token":"next"},"last_sync_cursor":"opaque","other":7}' WHERE id='a'`)
	if err := db.SetAccountAuthState(t.Context(), "a", domain.ConnectionPermissionRequired); err != nil {
		t.Fatal(err)
	}
	if state, err := db.AccountAuthState(t.Context(), "a"); err != nil || state != domain.ConnectionPermissionRequired {
		t.Fatal(state, err)
	}
	if state, _ := db.AccountAuthState(t.Context(), "b"); state != domain.ConnectionAvailable {
		t.Fatal("state crossed account boundary")
	}
	if ok, err := db.ReplaceAccountCredentials(t.Context(), "a", []byte("stale"), []byte("new")); err != nil || ok {
		t.Fatal("stale credentials replaced", ok, err)
	}
	if state, _ := db.AccountAuthState(t.Context(), "a"); state != domain.ConnectionPermissionRequired {
		t.Fatal("failed CAS cleared pause")
	}
	if ok, err := db.ReplaceAccountCredentials(t.Context(), "a", []byte("old"), []byte("new")); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if state, _ := db.AccountAuthState(t.Context(), "a"); state != domain.ConnectionAvailable {
		t.Fatal("replacement did not clear pause")
	}
	var raw string
	if err := db.read.QueryRow(`SELECT sync_state FROM accounts WHERE id='a'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != `{"gmail_checkpoint":{"phase":"messages","page_token":"next"},"last_sync_cursor":"opaque","other":7}` {
		t.Fatal("unrelated state changed", raw)
	}
	if err := db.DeleteAccount(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.ReplaceAccountCredentials(t.Context(), "a", nil, []byte("resurrect")); err != nil || ok {
		t.Fatal("deleted account resurrected", ok, err)
	}
}

func TestOutboxAccountExclusionAndAccessReleaseRetainRetryBudget(t *testing.T) {
	db := openStore(t)
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id, Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := db.QueueOutbox(t.Context(), OutboxRecord{ID: id, AccountID: id, RequestKey: id, Data: []byte("encrypted"), Summary: []byte("summary")}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().Unix()
	row, err := db.ClaimOutbox(t.Context(), now, "a")
	if err != nil || row.AccountID != "b" {
		t.Fatal("paused account blocked other account", row, err)
	}
	if err := db.BeginOutboxSend(t.Context(), row.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleOutbox(t.Context(), row.ID, "outcome_unknown", "unknown", "", false); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		row, err = db.ClaimOutbox(t.Context(), now)
		if err != nil || row.AccountID != "a" {
			t.Fatal(row, err)
		}
		if err := db.ReleaseOutboxAccessClaim(t.Context(), row.ID); err != nil {
			t.Fatal(err)
		}
		stored, err := db.Outbox(t.Context(), "a", row.ID)
		if err != nil || stored.Attempts != 0 || stored.Status != "queued" || !bytes.Equal(stored.Data, []byte("encrypted")) {
			t.Fatal("release damaged queued send", stored, err)
		}
	}
	if _, err := db.ClaimOutbox(t.Context(), now, "a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("paused or unknown send claimed", err)
	}
	if err := db.ReleaseOutboxAccessClaim(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	unknown, _ := db.Outbox(t.Context(), "b", "b")
	if unknown.Status != "outcome_unknown" {
		t.Fatal("access release changed unknown", unknown.Status)
	}
}
