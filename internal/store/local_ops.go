package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type LocalMailChange struct {
	MessageID     string
	IsRead        *bool
	IsStarred     *bool
	RemoveFolders []string
	AddFolders    []string
}

// ApplyLocalMailChanges is the cache side of one queued remote operation and
// runs inside the transaction that inserts its pending row.
func ApplyLocalMailChanges(ctx context.Context, tx *sql.Tx, changes []LocalMailChange) error {
	now := time.Now().Unix()
	for _, change := range changes {
		if change.MessageID == "" {
			return errors.New("empty message for local mail change")
		}
		if change.IsRead != nil {
			if _, err := tx.ExecContext(ctx, "UPDATE messages SET is_read=?,updated_at=? WHERE id=?", *change.IsRead, now, change.MessageID); err != nil {
				return err
			}
		}
		if change.IsStarred != nil {
			if _, err := tx.ExecContext(ctx, "UPDATE messages SET is_starred=?,updated_at=? WHERE id=?", *change.IsStarred, now, change.MessageID); err != nil {
				return err
			}
		}
		for _, folder := range change.RemoveFolders {
			if _, err := tx.ExecContext(ctx, "DELETE FROM message_folders WHERE message_id=? AND folder_id=?", change.MessageID, folder); err != nil {
				return err
			}
		}
		for _, folder := range change.AddFolders {
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO message_folders(message_id,folder_id) SELECT ?,id FROM folders WHERE id=?", change.MessageID, folder); err != nil {
				return err
			}
		}
	}
	return nil
}

func (db *DB) Folder(ctx context.Context, id string) (accountID, remoteID string, err error) {
	err = db.read.QueryRowContext(ctx, "SELECT account_id,remote_id FROM folders WHERE id=?", id).Scan(&accountID, &remoteID)
	return
}

func (db *DB) FolderByRemote(ctx context.Context, account, remoteID string) (string, error) {
	var id string
	err := db.read.QueryRowContext(ctx, "SELECT id FROM folders WHERE account_id=? AND remote_id=?", account, remoteID).Scan(&id)
	return id, err
}

func (db *DB) MessageAccount(ctx context.Context, id string) (string, error) {
	var account string
	err := db.read.QueryRowContext(ctx, "SELECT account_id FROM messages WHERE id=? AND is_deleted=0", id).Scan(&account)
	return account, err
}

// MessageInFolderRemote reports membership by the provider label, which is
// what the archive toggle means to Gmail.
func (db *DB) MessageInFolderRemote(ctx context.Context, messageID, remoteID string) (bool, error) {
	var exists bool
	err := db.read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM message_folders mf JOIN folders f ON f.id=mf.folder_id WHERE mf.message_id=? AND f.remote_id=?)`, messageID, remoteID).Scan(&exists)
	return exists, err
}

func (db *DB) UnreadMessageIDs(ctx context.Context, account string, folderIDs []string) ([]string, error) {
	query := "SELECT id FROM messages WHERE account_id=? AND is_read=0 AND is_deleted=0"
	args := []any{account}
	if len(folderIDs) > 0 {
		query = "SELECT DISTINCT m.id FROM messages m JOIN message_folders f ON f.message_id=m.id WHERE m.account_id=? AND m.is_read=0 AND m.is_deleted=0 AND f.folder_id IN (" + placeholders(len(folderIDs)) + ")"
		for _, id := range folderIDs {
			args = append(args, id)
		}
	}
	rows, err := db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for i := 1; i < n; i++ {
		out += ",?"
	}
	return out
}
