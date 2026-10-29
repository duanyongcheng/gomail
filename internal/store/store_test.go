package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func openStore(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func openRaw(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn(filepath.Join(t.TempDir(), "legacy.sqlite"), false))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func execute(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func integer(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var result int
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOpenProfileConnectionsAndPermissions(t *testing.T) {
	// URI punctuation and Unicode must name a real path, not DSN parameters.
	path := filepath.Join(t.TempDir(), "中文 ?#% profile", "mail ?#%.sqlite")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Path() != path {
		t.Fatalf("path = %q, want %q", db.Path(), path)
	}
	if integer(t, db.write, "PRAGMA user_version") != SchemaVersion {
		t.Fatal("wrong schema version")
	}
	var journal string
	if err := db.write.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal mode = %q, %v", journal, err)
	}
	for _, pool := range []*sql.DB{db.read, db.write} {
		if integer(t, pool, "PRAGMA foreign_keys") != 1 || integer(t, pool, "PRAGMA busy_timeout") != 15000 {
			t.Fatal("missing connection pragmas")
		}
	}
	if db.write.Stats().MaxOpenConnections != 1 || db.read.Stats().MaxOpenConnections != readConnections {
		t.Fatal("connection pools are not bounded")
	}
	if _, err := db.read.Exec("INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('bad','bad','gmail',0,0)"); err == nil {
		t.Fatal("read pool accepted a write")
	}
	if _, err := db.write.Exec("INSERT INTO folders(id,account_id,remote_id,name,folder_type) VALUES('bad','missing','bad','bad','folder')"); err == nil {
		t.Fatal("write pool did not enforce foreign keys")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("database permissions = %v", info.Mode())
	}
	for _, table := range []string{"protocol_search", "protocol_attachment_data", "gmail_cursor", "protocol_cursor", "protocol_map", "protocol_send"} {
		if integer(t, db.read, "SELECT count(*) FROM sqlite_master WHERE name=?", table) != 0 {
			t.Fatalf("prototype table created: %s", table)
		}
	}
}

func TestReadPoolContinuesDuringUncommittedWrite(t *testing.T) {
	db := openStore(t)
	execute(t, db.write, "INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','before','gmail',1,1)")
	tx, err := db.write.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE accounts SET email='after' WHERE id='a'"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, readConnections)
	for range readConnections {
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var email string
			err := db.read.QueryRowContext(ctx, "SELECT email FROM accounts WHERE id='a'").Scan(&email)
			if err == nil && email != "before" {
				err = errors.New("read observed an uncommitted write")
			}
			results <- err
		}()
	}
	for range readConnections {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var email string
	if err := db.read.QueryRow("SELECT email FROM accounts WHERE id='a'").Scan(&email); err != nil || email != "after" {
		t.Fatalf("committed email = %q, %v", email, err)
	}
}

func TestAuthDataCompareAndSwap(t *testing.T) {
	db := openStore(t)
	execute(t, db.write, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES
 ('a','a@example.test','gmail',1,1), ('b','b@example.test','gmail',1,1);`)
	if ok, err := db.CompareAndSwapAuthData(t.Context(), "a", nil, []byte("token-a"), 2); err != nil || !ok {
		t.Fatalf("initial CAS = %v, %v", ok, err)
	}
	if ok, err := db.CompareAndSwapAuthData(t.Context(), "a", []byte("stale"), []byte("token-b"), 3); err != nil || ok {
		t.Fatalf("stale CAS = %v, %v", ok, err)
	}
	if ok, err := db.CompareAndSwapAuthData(t.Context(), "a", []byte("token-a"), []byte("token-new"), 4); err != nil || !ok {
		t.Fatalf("refresh CAS = %v, %v", ok, err)
	}
	value, err := db.AuthData(t.Context(), "a")
	if err != nil || !bytes.Equal(value, []byte("token-new")) {
		t.Fatalf("auth_data = %q, %v", value, err)
	}
	other, err := db.AuthData(t.Context(), "b")
	if err != nil || other != nil {
		t.Fatalf("other account auth_data = %q, %v", other, err)
	}
	if _, err := db.AuthData(t.Context(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing account error = %v", err)
	}
	if integer(t, db.read, "SELECT updated_at FROM accounts WHERE id='a'") != 4 {
		t.Fatal("CAS did not update timestamp")
	}
}

func TestSearchTracksMessagesTransactionally(t *testing.T) {
	db := openStore(t)
	execute(t, db.write, "INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1)")
	execute(t, db.write, `INSERT INTO messages(id,account_id,remote_id,subject,body_text,date,created_at,updated_at)
 VALUES('m','a','r','中文主题','中文搜索 Hello world',1,1,1)`)
	search := func(query string, want int) {
		t.Helper()
		if got := integer(t, db.read, "SELECT count(*) FROM message_search WHERE message_search MATCH ?", query); got != want {
			t.Fatalf("search %q = %d, want %d", query, got, want)
		}
	}
	search(`"中文搜索"`, 1)
	search(`"HELLO"`, 1)
	expected := errors.New("injected failure after index update")
	if err := db.withTx(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE messages SET body_text='replacement' WHERE id='m'"); err != nil {
			return err
		}
		return expected
	}); !errors.Is(err, expected) {
		t.Fatal(err)
	}
	search(`"中文搜索"`, 1)
	search(`"replacement"`, 0)
	execute(t, db.write, "UPDATE messages SET body_text='updated text' WHERE id='m'")
	search(`"updated"`, 1)
	search(`"中文搜索"`, 0)
	execute(t, db.write, "UPDATE messages SET is_deleted=1 WHERE id='m'")
	search(`"updated"`, 0)
	execute(t, db.write, "UPDATE messages SET is_deleted=0 WHERE id='m'")
	search(`"updated"`, 1)
	execute(t, db.write, "DELETE FROM accounts WHERE id='a'")
	search(`"updated"`, 0)
}

type schemaObject struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Table string `json:"table"`
	SQL   string `json:"sql"`
}

type legacyGolden struct {
	Version int            `json:"version"`
	Objects []schemaObject `json:"objects"`
}

func readGolden(t *testing.T) legacyGolden {
	t.Helper()
	data, err := os.ReadFile("testdata/legacy_v20.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden legacyGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Version != LegacySchemaVersion || len(golden.Objects) == 0 {
		t.Fatal("invalid legacy schema oracle")
	}
	return golden
}

func checkLegacySchema(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT type,name,tbl_name,sql FROM sqlite_master
 WHERE name NOT LIKE 'sqlite_%' AND sql IS NOT NULL
 AND tbl_name NOT LIKE 'message_search%' AND name NOT LIKE 'trg_message_search_%'
 AND tbl_name NOT IN ('gmail_backfill_pending','gmail_message_failures','mail_op_sequence','compose_drafts','compose_attachments','compose_draft_attachments', 'outbox','message_threads','gmail_label_changes')
 ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var objects []schemaObject
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			t.Fatal(err)
		}
		object.SQL = strings.Join(strings.Fields(object.SQL), " ")
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := readGolden(t).Objects
	if !reflect.DeepEqual(objects, want) {
		for i, object := range objects {
			if i >= len(want) || object != want[i] {
				t.Errorf("object[%d] differs from Rust oracle: %+v", i, object)
				if i < len(want) {
					t.Logf("want: %+v", want[i])
				}
			}
		}
		t.Fatalf("schema differs: got %d objects, want %d", len(objects), len(want))
	}
}

func TestFreshAndReopenedSchemaMatchesRustV20Golden(t *testing.T) {
	db := openStore(t)
	checkLegacySchema(t, db.read)
	if err := migrate(t.Context(), db.write, SchemaVersion); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, db.read)
}
