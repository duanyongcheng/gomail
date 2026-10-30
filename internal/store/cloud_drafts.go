package store

import (
	"context"
	"database/sql"
	"errors"
)

// Data encrypts the per-write Message-ID and MIME digest. Pending is deliberately
// retained on restart: the server may have accepted a write before a crash.
type GmailDraftCopy struct {
	DraftID, AccountID, RemoteID, RemoteMessageID, State string
	OutboxID                                             string
	Revision                                             int64
	Data                                                 []byte
}

func (db *DB) GmailDraftCopy(ctx context.Context, account, id string) (GmailDraftCopy, error) {
	var c GmailDraftCopy
	err := db.read.QueryRowContext(ctx, `SELECT draft_id,account_id,remote_id,remote_message_id,revision,state,data,COALESCE(outbox_id,'') FROM gmail_draft_copies WHERE account_id=? AND draft_id=?`, account, id).Scan(&c.DraftID, &c.AccountID, &c.RemoteID, &c.RemoteMessageID, &c.Revision, &c.State, &c.Data, &c.OutboxID)
	return c, err
}
func (db *DB) BeginGmailDraftCopy(ctx context.Context, c GmailDraftCopy) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT d.revision FROM compose_drafts d JOIN accounts a ON a.id=d.account_id WHERE d.id=? AND d.account_id=? AND d.deleted=0 AND a.provider='gmail'`, c.DraftID, c.AccountID).Scan(&revision); err != nil {
			return err
		}
		if revision != c.Revision || len(c.Data) == 0 {
			return ErrDraftConflict
		}
		var state, remote, message string
		err := tx.QueryRowContext(ctx, `SELECT state,remote_id,remote_message_id FROM gmail_draft_copies WHERE draft_id=? AND account_id=?`, c.DraftID, c.AccountID).Scan(&state, &remote, &message)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (state != "synced" || remote != c.RemoteID || message != c.RemoteMessageID) {
			return ErrDraftConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO gmail_draft_copies(draft_id,account_id,remote_id,remote_message_id,revision,state,data) VALUES(?,?,?,?,?,'pending',?) ON CONFLICT(draft_id) DO UPDATE SET revision=excluded.revision,state='pending',data=excluded.data`, c.DraftID, c.AccountID, c.RemoteID, c.RemoteMessageID, c.Revision, c.Data)
		return err
	})
}
func (db *DB) SettleGmailDraftCopy(ctx context.Context, c GmailDraftCopy) error {
	if c.State != "synced" && c.State != "conflict" {
		return errors.New("invalid cloud draft state")
	}
	result, err := db.write.ExecContext(ctx, `UPDATE gmail_draft_copies SET remote_id=?,remote_message_id=?,state=? WHERE draft_id=? AND account_id=? AND revision=? AND data=? AND state IN ('pending','synced','conflict') AND outbox_id IS NULL`, c.RemoteID, c.RemoteMessageID, c.State, c.DraftID, c.AccountID, c.Revision, c.Data)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrDraftConflict
	}
	return err
}

// Explicit rejection restores the previous state; no retry starts here.
func (db *DB) RejectGmailDraftCopy(ctx context.Context, pending GmailDraftCopy, previous *GmailDraftCopy) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM gmail_draft_copies WHERE draft_id=? AND account_id=? AND state='pending' AND revision=? AND data=?`, pending.DraftID, pending.AccountID, pending.Revision, pending.Data)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrDraftConflict
		}
		if previous == nil {
			return nil
		}
		c := previous
		_, err = tx.ExecContext(ctx, `INSERT INTO gmail_draft_copies(draft_id,account_id,remote_id,remote_message_id,revision,state,data) VALUES(?,?,?,?,?,?,?)`, c.DraftID, c.AccountID, c.RemoteID, c.RemoteMessageID, c.Revision, c.State, c.Data)
		return err
	})
}
