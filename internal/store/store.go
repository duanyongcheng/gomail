// Package store owns profile persistence and schema compatibility with Pebble.
// Business packages use repository methods; SQL and connection pools stay here.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

import _ "modernc.org/sqlite"

const (
	LegacySchemaVersion = 20
	SchemaVersion       = 28
	readConnections     = 4
)

// DB serializes writes on one connection and serves concurrent reads through
// a separate pool of read-only connections. It has no desktop dependency.
type DB struct {
	write *sql.DB
	read  *sql.DB
	path  string
}

// Open opens a file database and migrates it before opening the read pool.
// Callers must supply the Pebble-Go profile path, never an old live profile.
func Open(ctx context.Context, path string) (_ *DB, err error) {
	if path == "" || path == ":memory:" {
		return nil, errors.New("profile database requires a file path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// Create the database with private permissions before SQLite can create a
	// database (and its WAL files) with process umask permissions.
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		err = file.Close()
	}
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create database file: %w", err)
	}

	write, err := sql.Open("sqlite", dsn(absolute, false))
	if err != nil {
		return nil, fmt.Errorf("open write database: %w", err)
	}
	defer func() {
		if err != nil {
			_ = write.Close()
		}
	}()
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	if err = write.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping write database: %w", err)
	}
	if err = migrate(ctx, write, SchemaVersion); err != nil {
		return nil, err
	}
	read, err := sql.Open("sqlite", dsn(absolute, true))
	if err != nil {
		return nil, fmt.Errorf("open read database: %w", err)
	}
	read.SetMaxOpenConns(readConnections)
	read.SetMaxIdleConns(readConnections)
	if err = read.PingContext(ctx); err != nil {
		_ = read.Close()
		return nil, fmt.Errorf("ping read database: %w", err)
	}
	return &DB{write: write, read: read, path: absolute}, nil
}

func dsn(path string, readOnly bool) string {
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := url.Values{
		"_pragma": {"busy_timeout(15000)", "foreign_keys(ON)", "synchronous(NORMAL)"},
	}
	if readOnly {
		query.Set("mode", "ro")
		query.Add("_pragma", "query_only(ON)")
	} else {
		query.Set("mode", "rwc")
		query.Add("_pragma", "journal_mode(WAL)")
		query.Set("_txlock", "immediate")
	}
	uri.RawQuery = query.Encode()
	return uri.String()
}

func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	return errors.Join(db.read.Close(), db.write.Close())
}

func (db *DB) Path() string { return db.path }

func (db *DB) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// AuthData returns the ciphertext without interpreting or logging it. A missing
// account returns sql.ErrNoRows, distinguishing deletion from absent credentials.
func (db *DB) AuthData(ctx context.Context, accountID string) ([]byte, error) {
	var value []byte
	err := db.read.QueryRowContext(ctx, "SELECT auth_data FROM accounts WHERE id=?", accountID).Scan(&value)
	return value, err
}

// CompareAndSwapAuthData prevents a stale refresher from overwriting new tokens.
// nil oldValue matches SQL NULL; an empty BLOB remains a distinct value.
func (db *DB) CompareAndSwapAuthData(ctx context.Context, accountID string, oldValue, newValue []byte, now int64) (bool, error) {
	result, err := db.write.ExecContext(ctx, `UPDATE accounts SET auth_data=?, updated_at=?
		WHERE id=? AND auth_data IS ?`, newValue, now, accountID, oldValue)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}
