package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (db *DB) GmailDraftCopyByRemote(ctx context.Context, account, remote string) (GmailDraftCopy, error) {
	var c GmailDraftCopy
	err := db.read.QueryRowContext(ctx, `SELECT draft_id,account_id,remote_id,remote_message_id,revision,state,data,COALESCE(outbox_id,'')
		FROM gmail_draft_copies WHERE account_id=? AND remote_id=?`, account, remote).
		Scan(&c.DraftID, &c.AccountID, &c.RemoteID, &c.RemoteMessageID, &c.Revision, &c.State, &c.Data, &c.OutboxID)
	return c, err
}

func (db *DB) GmailDraftLocalID(ctx context.Context, account, remote string) (string, error) {
	var id string
	err := db.read.QueryRowContext(ctx, `SELECT c.draft_id FROM gmail_draft_copies c
		JOIN compose_drafts d ON d.id=c.draft_id AND d.account_id=c.account_id
		WHERE c.account_id=? AND c.remote_id=? AND d.deleted=0`, account, remote).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func oneCloudRow(result sql.Result, err error) error {
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
	return nil
}

func (db *DB) BeginGmailDraftDelete(ctx context.Context, c GmailDraftCopy) error {
	if c.State != "synced" || c.RemoteID == "" || c.OutboxID != "" {
		return ErrDraftConflict
	}
	return oneCloudRow(db.write.ExecContext(ctx, `UPDATE gmail_draft_copies SET state='delete_pending'
		WHERE draft_id=? AND account_id=? AND remote_id=? AND remote_message_id=? AND revision=? AND data=?
		AND state='synced' AND outbox_id IS NULL AND EXISTS(
			SELECT 1 FROM compose_drafts d WHERE d.id=draft_id AND d.account_id=? AND d.deleted=0)`,
		c.DraftID, c.AccountID, c.RemoteID, c.RemoteMessageID, c.Revision, c.Data, c.AccountID))
}

func (db *DB) RejectGmailDraftDelete(ctx context.Context, c GmailDraftCopy) error {
	return oneCloudRow(db.write.ExecContext(ctx, `UPDATE gmail_draft_copies SET state='synced'
		WHERE draft_id=? AND account_id=? AND remote_id=? AND revision=? AND data=?
		AND state='delete_pending' AND outbox_id IS NULL`,
		c.DraftID, c.AccountID, c.RemoteID, c.Revision, c.Data))
}

// A cloud association is forgotten only after a confirmed deletion or an
// explicit resolution of a known conflict. Unknown writes are never detached.
func (db *DB) RemoveGmailDraftCopy(ctx context.Context, c GmailDraftCopy) error {
	if c.State != "conflict" && c.State != "delete_pending" {
		return ErrDraftConflict
	}
	return oneCloudRow(db.write.ExecContext(ctx, `DELETE FROM gmail_draft_copies
		WHERE draft_id=? AND account_id=? AND remote_id=? AND remote_message_id=? AND revision=? AND data=?
		AND state=? AND outbox_id IS NULL`,
		c.DraftID, c.AccountID, c.RemoteID, c.RemoteMessageID, c.Revision, c.Data, c.State))
}

// ImportGmailDraft commits encrypted content, attachment ownership and the
// stable cloud identity together. Transferring a conflict leaves its old local
// draft untouched so recovery never overwrites the user's unsent edits.
func (db *DB) ImportGmailDraft(ctx context.Context, d EncryptedDraft, attachments []EncryptedComposeAttachment, c GmailDraftCopy, previous *GmailDraftCopy) error {
	if d.ID == "" || d.AccountID == "" || d.Revision != 0 || len(d.Data) == 0 ||
		c.DraftID != d.ID || c.AccountID != d.AccountID || c.RemoteID == "" ||
		c.RemoteMessageID == "" || c.State != "synced" || c.Revision != 1 || len(c.Data) == 0 || c.OutboxID != "" {
		return ErrDraftConflict
	}
	if len(attachments) > MaxComposeAttachments {
		return errors.New("draft attachments exceed limit")
	}
	var total int64
	seen := map[string]bool{}
	for _, a := range attachments {
		if a.ID == "" || seen[a.ID] || a.Size < 0 || a.Size > MaxComposeAttachmentBytes || len(a.Data) == 0 {
			return errors.New("invalid imported attachment")
		}
		seen[a.ID] = true
		total += a.Size
	}
	if total > MaxDraftAttachmentBytes {
		return errors.New("draft attachments exceed limit")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var gmail bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=? AND provider='gmail')`, d.AccountID).Scan(&gmail); err != nil {
			return err
		}
		if !gmail {
			return errors.New("Gmail account required")
		}
		var used int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM compose_attachments`).Scan(&used); err != nil {
			return err
		}
		if used+total > MaxComposeStagingBytes {
			return errors.New("compose attachment storage limit reached")
		}
		if previous != nil {
			p := previous
			if p.AccountID != c.AccountID || p.RemoteID != c.RemoteID || p.DraftID == d.ID ||
				(p.State != "synced" && p.State != "conflict") || p.OutboxID != "" {
				return ErrDraftConflict
			}
			if err := oneCloudRow(tx.ExecContext(ctx, `DELETE FROM gmail_draft_copies
				WHERE draft_id=? AND account_id=? AND remote_id=? AND remote_message_id=? AND revision=? AND data=?
				AND state=? AND outbox_id IS NULL`,
				p.DraftID, p.AccountID, p.RemoteID, p.RemoteMessageID, p.Revision, p.Data, p.State)); err != nil {
				return err
			}
		}
		now := time.Now().Unix()
		if _, err := tx.ExecContext(ctx, `INSERT INTO compose_drafts
			(id,account_id,request_key,data,revision,deleted,updated_at) VALUES(?,?,?,?,1,0,?)`,
			d.ID, d.AccountID, d.RequestKey, d.Data, now); err != nil {
			return err
		}
		for _, a := range attachments {
			if _, err := tx.ExecContext(ctx, `INSERT INTO compose_attachments
				(id,data,size,released,created_at) VALUES(?,?,?,1,?)`, a.ID, a.Data, a.Size, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO compose_draft_attachments(draft_id,attachment_id) VALUES(?,?)`, d.ID, a.ID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO gmail_draft_copies
			(draft_id,account_id,remote_id,remote_message_id,revision,state,data) VALUES(?,?,?,?,1,'synced',?)`,
			c.DraftID, c.AccountID, c.RemoteID, c.RemoteMessageID, c.Data)
		return err
	})
}
