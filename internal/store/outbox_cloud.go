package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// The immutable copy observed while building MIME is reserved in the same
// transaction as the encrypted outbox insert. The latest local snapshot can
// be newer than the cloud copy, but is protected from edits/deletion once reserved.
type OutboxDraftReservation struct {
	Copy          GmailDraftCopy
	LocalRevision int64
}

func reserveOutboxDraft(ctx context.Context, tx *sql.Tx, record OutboxRecord) error {
	if record.CloudDraft == nil {
		return nil
	}
	expected := record.CloudDraft
	copy := expected.Copy
	if copy.AccountID != record.AccountID || copy.State != "synced" || copy.RemoteID == "" || copy.RemoteMessageID == "" || len(copy.Data) == 0 {
		return ErrDraftConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE gmail_draft_copies SET state='sending',outbox_id=?
		WHERE draft_id=? AND account_id=? AND state='synced' AND outbox_id IS NULL
		AND remote_id=? AND remote_message_id=? AND revision=? AND data=?
		AND EXISTS(SELECT 1 FROM compose_drafts d WHERE d.id=gmail_draft_copies.draft_id
			AND d.account_id=gmail_draft_copies.account_id AND d.deleted=0 AND d.revision=?)`,
		record.ID, copy.DraftID, record.AccountID, copy.RemoteID, copy.RemoteMessageID, copy.Revision, copy.Data, expected.LocalRevision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrDraftConflict
	}
	return err
}

func beginOutboxSend(ctx context.Context, tx *sql.Tx, id string) error {
	result, err := tx.ExecContext(ctx, "UPDATE outbox SET status='in_progress',updated_at=? WHERE id=? AND status='preparing'", time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("outbox item no longer available")
	}
	return err
}

func (db *DB) BeginCloudOutboxSend(ctx context.Context, account, id, draftID, remoteID, messageID string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var reserved bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM gmail_draft_copies c JOIN outbox o ON o.id=c.outbox_id
			WHERE c.account_id=? AND c.outbox_id=? AND c.draft_id=?
			AND c.remote_id=? AND c.remote_message_id=? AND c.state='sending'
			AND o.account_id=c.account_id AND o.status='preparing'
		)`, account, id, draftID, remoteID, messageID).Scan(&reserved)
		if err != nil {
			return err
		}
		if !reserved {
			return ErrDraftConflict
		}
		return beginOutboxSend(ctx, tx, id)
	})
}

func settleOutboxDraft(ctx context.Context, tx *sql.Tx, id, status string) error {
	if status != "sent" {
		return nil
	}
	var draftID string
	err := tx.QueryRowContext(ctx, `SELECT draft_id FROM gmail_draft_copies WHERE outbox_id=? AND state='sending'`, id).Scan(&draftID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE gmail_draft_copies SET state='sent' WHERE outbox_id=? AND state='sending'`, id); err != nil {
		return err
	}
	// Acceptance, association and cleanup commit together. Any failure rolls
	// back to the protected reservation, so retrying SQLite cannot resend.
	// Keep both the send-key tombstone and the sent association permanently.
	return tombstoneComposeDraft(ctx, tx, draftID)
}
