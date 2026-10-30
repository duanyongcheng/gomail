package store

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

func assertDurabilityPragmas(t *testing.T, conn *sql.Conn, reader bool) {
	t.Helper()
	var journal string
	if err := conn.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal_mode = %q, err = %v; want wal", journal, err)
	}
	wantSync, wantReadOnly := 2, 0 // SQLite FULL = 2, NORMAL = 1.
	if reader {
		wantSync, wantReadOnly = 1, 1
	}
	for pragma, want := range map[string]int{
		"synchronous":  wantSync,
		"query_only":   wantReadOnly,
		"foreign_keys": 1,
		"busy_timeout": 15000,
	} {
		var got int
		if err := conn.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("reader=%v PRAGMA %s = %d, err = %v; want %d", reader, pragma, got, err, want)
		}
	}
}

func assertDurabilityPools(t *testing.T, db *DB) {
	t.Helper()
	writer, err := db.write.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	assertDurabilityPragmas(t, writer, false)
	// Hold all readers simultaneously; repeated pool queries could otherwise
	// inspect the same physical connection four times.
	for range readConnections {
		reader, err := db.read.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		assertDurabilityPragmas(t, reader, true)
	}
}

func replaceDurabilityWriter(t *testing.T, db *DB, badConn bool) {
	t.Helper()
	conn, err := db.write.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Both settings are connection-local. The replacement must restore FULL
	// from the DSN, not inherit a one-time PRAGMA applied by Open.
	if _, err := conn.ExecContext(t.Context(), "PRAGMA synchronous=NORMAL; CREATE TEMP TABLE retired_writer(value)"); err != nil {
		t.Fatal(err)
	}
	if badConn {
		// database/sql documents ErrBadConn from Raw as discarding the driver
		// connection. Do not close the driver directly behind the pool's back.
		if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
			t.Fatal(err)
		}
	} else {
		db.write.SetMaxIdleConns(0)
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		db.write.SetMaxIdleConns(1)
	}
	if got := integer(t, db.write, "SELECT count(*) FROM sqlite_temp_master WHERE name='retired_writer'"); got != 0 {
		t.Fatal("writer was not physically replaced")
	}
}

func TestWriterDurabilityAcrossReplacementAndReopen(t *testing.T) {
	db := outboxFixture(t)
	ctx := t.Context()
	ids := []string{}
	verify := func(view *DB) {
		t.Helper()
		for _, id := range ids {
			outbox, err := view.Outbox(ctx, "a", id)
			if err != nil || outbox.Status != "in_progress" || outbox.Attempts != 1 || !bytes.Equal(outbox.Data, []byte("cipher")) || !bytes.Equal(outbox.Summary, []byte("summary")) {
				t.Fatalf("persisted outbox %s: %+v, %v", id, outbox, err)
			}
			copy, err := view.GmailDraftCopy(ctx, "a", id)
			if err != nil || copy.State != "pending" || copy.Revision != 1 || !bytes.Equal(copy.Data, []byte("encrypted intent "+id)) {
				t.Fatalf("persisted cloud intent %s: %+v, %v", id, copy, err)
			}
		}
	}
	writeIntents := func(writer *DB, id string) {
		t.Helper()
		assertDurabilityPools(t, writer)
		queueOutbox(t, writer, id)
		row, err := writer.ClaimOutbox(ctx, time.Now().Unix())
		if err != nil || row.ID != id {
			t.Fatalf("claim = %s, %v", row.ID, err)
		}
		if err := writer.BeginOutboxSend(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := writer.SaveComposeDraft(ctx, EncryptedDraft{ID: id, AccountID: "a", RequestKey: id, Data: []byte("encrypted draft")}, nil); err != nil {
			t.Fatal(err)
		}
		if err := writer.BeginGmailDraftCopy(ctx, GmailDraftCopy{DraftID: id, AccountID: "a", Revision: 1, Data: []byte("encrypted intent " + id)}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		verify(writer)
	}

	writeIntents(db, "initial")
	replaceDurabilityWriter(t, db, true)
	writeIntents(db, "bad-connection-replacement")
	replaceDurabilityWriter(t, db, false)
	writeIntents(db, "idle-connection-replacement")

	// A completely independent pool must use FULL too, and see committed WAL
	// content while the original pools are still open (no close checkpoint).
	independent, err := Open(ctx, db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close()
	verify(independent)
	writeIntents(independent, "independent-pool")
	verify(db)
	if err := independent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	verify(reopened)
	writeIntents(reopened, "closed-and-reopened")
	if err := reopened.RecoverOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		row, err := reopened.Outbox(ctx, "a", id)
		if err != nil || row.Status != "outcome_unknown" || row.Retryable || row.NextRetryAt != nil {
			t.Fatalf("recovery of %s = %+v, %v", id, row, err)
		}
		copy, err := reopened.GmailDraftCopy(ctx, "a", id)
		if err != nil || copy.State != "pending" {
			t.Fatalf("cloud recovery of %s = %+v, %v", id, copy, err)
		}
		if err := reopened.BeginGmailDraftCopy(ctx, copy); !errors.Is(err, ErrDraftConflict) {
			t.Fatalf("unconfirmed cloud intent became retryable: %v", err)
		}
	}
	for range 2 {
		if _, err := reopened.ClaimOutbox(ctx, time.Now().Add(24*time.Hour).Unix()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("recovered unknown send was claimed: %v", err)
		}
	}
	// These are real connection/commit/reopen tests, not a simulation of power
	// loss or a claim that the operating system and disk honor every sync.
}
