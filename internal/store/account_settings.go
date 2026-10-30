package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type AccountSettings struct {
	AccountID, Email, DisplayName string
	Color, Label                  *string
	Signature                     []byte // nil preserves the existing encrypted signature
}

// UpdateGmailSettings updates only local preferences. Identity, authorization,
// sync state and queues are never copied back from a stale settings form.
func (db *DB) UpdateGmailSettings(ctx context.Context, settings AccountSettings) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var email, provider string
		if err := tx.QueryRowContext(ctx, "SELECT email,provider FROM accounts WHERE id=?", settings.AccountID).Scan(&email, &provider); err != nil {
			return err
		}
		if provider != "gmail" || !strings.EqualFold(email, settings.Email) {
			return errors.New("Gmail mailbox address cannot be changed in account settings")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET display_name=?,color=COALESCE(?,color),account_label=COALESCE(?,account_label),updated_at=? WHERE id=?`, settings.DisplayName, settings.Color, settings.Label, time.Now().Unix(), settings.AccountID); err != nil {
			return err
		}
		if settings.Signature != nil {
			_, err := tx.ExecContext(ctx, `INSERT INTO email_signatures(account_id,data) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET data=excluded.data`, settings.AccountID, settings.Signature)
			return err
		}
		return nil
	})
}

// A missing signature for an existing account returns nil. A removed account is
// an error, so callers cannot accidentally migrate data into a recreated account.
func (db *DB) EmailSignature(ctx context.Context, account string) ([]byte, error) {
	var data []byte
	err := db.read.QueryRowContext(ctx, `SELECT s.data FROM accounts a LEFT JOIN email_signatures s ON s.account_id=a.id WHERE a.id=?`, account).Scan(&data)
	return data, err
}

// Migrations use insert-if-absent even for an explicitly empty signature. Return
// the winning ciphertext in the same transaction, including a concurrent save.
func (db *DB) SaveEmailSignature(ctx context.Context, account string, data []byte, onlyAbsent bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("encrypted signature required")
	}
	var stored []byte
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		query := `INSERT INTO email_signatures(account_id,data) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET data=excluded.data`
		if onlyAbsent {
			query = `INSERT INTO email_signatures(account_id,data) VALUES(?,?) ON CONFLICT(account_id) DO NOTHING`
		}
		if _, err := tx.ExecContext(ctx, query, account, data); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT data FROM email_signatures WHERE account_id=?", account).Scan(&stored)
	})
	return stored, err
}
