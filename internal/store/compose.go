package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const MaxComposeAttachmentBytes = 10 << 20
const MaxDraftAttachmentBytes = 25 << 20
const MaxComposeStagingBytes = 200 << 20
const MaxComposeAttachments = 100

type EncryptedDraft struct {
	ID, AccountID, RequestKey string
	Data                      []byte
	Revision, UpdatedAt       int64
	Deleted                   bool
}
type EncryptedComposeAttachment struct {
	ID   string
	Data []byte
	Size int64
}

var ErrDraftConflict = errors.New("draft changed or was deleted; reopen it before saving")

func (db *DB) HasEncryptedData(ctx context.Context) (bool, error) {
	var has bool
	err := db.read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE auth_data IS NOT NULL)
		OR EXISTS(SELECT 1 FROM compose_drafts WHERE data IS NOT NULL)
		OR EXISTS(SELECT 1 FROM compose_attachments)
		OR EXISTS(SELECT 1 FROM outbox WHERE data IS NOT NULL)`).Scan(&has)
	return has, err
}
func (db *DB) ComposeAccountExists(ctx context.Context, account string) (bool, error) {
	var exists bool
	err := db.read.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=?)", account).Scan(&exists)
	return exists, err
}
func scanDraft(row interface{ Scan(...any) error }) (EncryptedDraft, error) {
	var d EncryptedDraft
	err := row.Scan(&d.ID, &d.AccountID, &d.RequestKey, &d.Data, &d.Revision, &d.Deleted, &d.UpdatedAt)
	return d, err
}

const draftColumns = "id,account_id,request_key,data,revision,deleted,updated_at"

func (db *DB) ComposeDraft(ctx context.Context, account, id string) (EncryptedDraft, error) {
	return scanDraft(db.read.QueryRowContext(ctx, "SELECT "+draftColumns+" FROM compose_drafts WHERE account_id=? AND id=?", account, id))
}
func (db *DB) ComposeDraftByKey(ctx context.Context, account, key string) (EncryptedDraft, error) {
	return scanDraft(db.read.QueryRowContext(ctx, "SELECT "+draftColumns+" FROM compose_drafts WHERE account_id=? AND request_key=?", account, key))
}
func (db *DB) ComposeDrafts(ctx context.Context, account string) ([]EncryptedDraft, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT "+draftColumns+" FROM compose_drafts WHERE account_id=? AND deleted=0 ORDER BY updated_at DESC,id LIMIT 100", account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EncryptedDraft{}
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveComposeDraft uses a revision CAS and replaces attachment references in the
// same transaction. A tombstone prevents a late autosave from resurrecting a
// deleted draft. Repeating the same client key resolves to the original ID.
func (db *DB) SaveComposeDraft(ctx context.Context, d EncryptedDraft, attachments []string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var revision int64
		var deleted bool
		err := tx.QueryRowContext(ctx, "SELECT revision,deleted FROM compose_drafts WHERE id=? AND account_id=?", d.ID, d.AccountID).Scan(&revision, &deleted)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (deleted || revision != d.Revision) {
			return ErrDraftConflict
		}
		if errors.Is(err, sql.ErrNoRows) && d.Revision != 0 {
			return ErrDraftConflict
		}
		var total int64
		seen := map[string]bool{}
		for _, id := range attachments {
			if seen[id] {
				return errors.New("duplicate compose attachment")
			}
			seen[id] = true
			var size int64
			if err := tx.QueryRowContext(ctx, "SELECT size FROM compose_attachments WHERE id=?", id).Scan(&size); err != nil {
				return errors.New("compose attachment is unavailable")
			}
			total += size
		}
		if len(attachments) > MaxComposeAttachments || total > MaxDraftAttachmentBytes {
			return errors.New("draft attachments exceed limit")
		}
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, "INSERT INTO compose_drafts(id,account_id,request_key,data,revision,deleted,updated_at) VALUES(?,?,?,?,1,0,?)", d.ID, d.AccountID, d.RequestKey, d.Data, time.Now().Unix())
		} else {
			_, err = tx.ExecContext(ctx, "UPDATE compose_drafts SET data=?,revision=revision+1,updated_at=? WHERE id=? AND account_id=?", d.Data, time.Now().Unix(), d.ID, d.AccountID)
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE compose_attachments SET released=1 WHERE id IN (SELECT attachment_id FROM compose_draft_attachments WHERE draft_id=?)", d.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM compose_draft_attachments WHERE draft_id=?", d.ID); err != nil {
			return err
		}
		for _, id := range attachments {
			if _, err := tx.ExecContext(ctx, "INSERT INTO compose_draft_attachments(draft_id,attachment_id) VALUES(?,?)", d.ID, id); err != nil {
				return err
			}
		}
		return cleanReleasedComposeAttachments(ctx, tx)
	})
}
func (db *DB) DeleteComposeDraft(ctx context.Context, account, id string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var owner string
		err := tx.QueryRowContext(ctx, "SELECT account_id FROM compose_drafts WHERE id=?", id).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if owner != account {
			return errors.New("draft belongs to another account")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE compose_drafts SET data=NULL,deleted=1,revision=revision+1,updated_at=? WHERE id=?", time.Now().Unix(), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE compose_attachments SET released=1 WHERE id IN (SELECT attachment_id FROM compose_draft_attachments WHERE draft_id=?)", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM compose_draft_attachments WHERE draft_id=?", id); err != nil {
			return err
		}
		return cleanReleasedComposeAttachments(ctx, tx)
	})
}
func (db *DB) StageComposeAttachment(ctx context.Context, a EncryptedComposeAttachment) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if a.Size < 0 || a.Size > MaxComposeAttachmentBytes {
			return errors.New("compose attachment exceeds 10 MiB")
		}
		var used int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(size),0) FROM compose_attachments").Scan(&used); err != nil {
			return err
		}
		if used+a.Size > MaxComposeStagingBytes {
			return errors.New("compose attachment storage limit reached")
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO compose_attachments(id,data,size,created_at) VALUES(?,?,?,?)", a.ID, a.Data, a.Size, time.Now().Unix())
		return err
	})
}
func (db *DB) ComposeAttachment(ctx context.Context, id string) (EncryptedComposeAttachment, error) {
	var a EncryptedComposeAttachment
	err := db.read.QueryRowContext(ctx, "SELECT id,data,size FROM compose_attachments WHERE id=?", id).Scan(&a.ID, &a.Data, &a.Size)
	return a, err
}
func (db *DB) ReleaseComposeAttachment(ctx context.Context, id string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE compose_attachments SET released=1 WHERE id=?", id); err != nil {
			return err
		}
		return cleanReleasedComposeAttachments(ctx, tx)
	})
}
func cleanReleasedComposeAttachments(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM compose_attachments WHERE released=1 AND NOT EXISTS(SELECT 1 FROM compose_draft_attachments r WHERE r.attachment_id=compose_attachments.id)`)
	return err
}
