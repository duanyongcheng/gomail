package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

type GmailLabelChange struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	RemoteID  string `json:"remoteId"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

func (db *DB) GmailLabelChange(ctx context.Context, account string) (*GmailLabelChange, error) {
	var c GmailLabelChange
	err := db.read.QueryRowContext(ctx, "SELECT id,action,remote_id,name,created_at FROM gmail_label_changes WHERE account_id=?", account).Scan(&c.ID, &c.Action, &c.RemoteID, &c.Name, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
func (db *DB) BeginGmailLabelChange(ctx context.Context, account string, change GmailLabelChange) (string, error) {
	id := rand.Text()
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if change.Action == "delete" {
			var active bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pending_mail_ops p
				WHERE p.account_id=? AND p.status!='done' AND CASE WHEN json_valid(p.payload_json) THEN
				EXISTS(SELECT 1 FROM json_each(p.payload_json,'$.add_labels') WHERE value=?) OR
				EXISTS(SELECT 1 FROM json_each(p.payload_json,'$.remove_labels') WHERE value=?) ELSE 0 END)`, account, change.RemoteID, change.RemoteID).Scan(&active); err != nil {
				return err
			}
			if active {
				return errors.New("label is used by pending mail changes; finish or dismiss those changes first")
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO gmail_label_changes(account_id,id,action,remote_id,name,created_at) VALUES(?,?,?,?,?,?)", account, id, change.Action, change.RemoteID, change.Name, time.Now().Unix())
		return err
	})
	return id, err
}

// Clear uses an observed ID so a stale UI/request cannot clear a newer action.
func (db *DB) ClearGmailLabelChange(ctx context.Context, account, id string) error {
	_, err := db.write.ExecContext(ctx, "DELETE FROM gmail_label_changes WHERE account_id=? AND id=?", account, id)
	return err
}

// ReconcileGmailLabels accepts only complete validated label lists from the
// provider. It never deletes messages, FTS rows, attachments or system fallbacks.
func (db *DB) ReconcileGmailLabels(ctx context.Context, account string, folders []domain.Folder) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=? AND provider='gmail')", account).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("Gmail account is unavailable")
		}
		seen := map[string]bool{}
		inbox := false
		for _, f := range folders {
			if f.AccountID != account || f.ID != ScopedID(account, "label:"+f.RemoteID) || f.RemoteID == "" || seen[f.RemoteID] || f.FolderType != "label" {
				return errors.New("invalid Gmail label snapshot")
			}
			seen[f.RemoteID] = true
			if f.RemoteID == "INBOX" && f.IsSystem {
				inbox = true
			}
		}
		if !inbox {
			return errors.New("incomplete Gmail label snapshot")
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,remote_id FROM folders WHERE account_id=? AND folder_type='label' AND is_system=0 AND role IS NULL", account)
		if err != nil {
			return err
		}
		type missing struct{ id, remote string }
		var removed []missing
		for rows.Next() {
			var f missing
			if err := rows.Scan(&f.id, &f.remote); err != nil {
				rows.Close()
				return err
			}
			if !seen[f.remote] && !domain.GmailSystemLabel(f.remote) {
				removed = append(removed, f)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, f := range folders {
			if _, err := tx.ExecContext(ctx, `INSERT INTO folders(id,account_id,remote_id,name,folder_type,role,is_system) VALUES(?,?,?,?,?,?,?)
				ON CONFLICT(id) DO UPDATE SET name=excluded.name,role=excluded.role,is_system=excluded.is_system WHERE folders.account_id=excluded.account_id`, f.ID, account, f.RemoteID, f.Name, f.FolderType, f.Role, f.IsSystem); err != nil {
				return err
			}
		}
		for _, f := range removed {
			// An externally removed target cannot be safely replayed. Retain the
			// failure for the user and block its later same-message operations.
			if _, err := tx.ExecContext(ctx, `UPDATE pending_mail_ops SET status='failed',attempts=?,next_retry_at=NULL,last_error='Target Gmail label was deleted; dismiss this change before continuing.',updated_at=?
				WHERE account_id=? AND status!='done' AND CASE WHEN json_valid(payload_json) THEN
				EXISTS(SELECT 1 FROM json_each(payload_json,'$.add_labels') WHERE value=?) OR
				EXISTS(SELECT 1 FROM json_each(payload_json,'$.remove_labels') WHERE value=?) ELSE 0 END`, MaxPendingMailOpAttempts, time.Now().Unix(), account, f.remote, f.remote); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM folders WHERE id=? AND account_id=?", f.id, account); err != nil {
				return err
			}
		}
		var c GmailLabelChange
		err = tx.QueryRowContext(ctx, "SELECT id,action,remote_id,name,created_at FROM gmail_label_changes WHERE account_id=?", account).Scan(&c.ID, &c.Action, &c.RemoteID, &c.Name, &c.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		matched := false
		switch c.Action {
		case "delete":
			matched = !seen[c.RemoteID]
		case "rename":
			for _, f := range folders {
				if f.RemoteID == c.RemoteID && !f.IsSystem && f.Name == c.Name {
					matched = true
				}
			}
		case "create":
			count := 0
			for _, f := range folders {
				if !f.IsSystem && f.Name == c.Name {
					count++
				}
			}
			matched = count == 1
		}
		if matched {
			_, err = tx.ExecContext(ctx, "DELETE FROM gmail_label_changes WHERE account_id=? AND id=?", account, c.ID)
		}
		return err
	})
}

func (db *DB) ValidateGmailLabelTargets(ctx context.Context, account string, payload MailOpPayload) error {
	for _, remote := range append(append([]string{}, payload.AddLabels...), payload.RemoveLabels...) {
		if domain.GmailSystemLabel(remote) {
			continue
		}
		var exists bool
		if err := db.read.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM folders WHERE account_id=? AND remote_id=? AND folder_type='label')", account, remote).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("target Gmail label is no longer available")
		}
	}
	return nil
}
