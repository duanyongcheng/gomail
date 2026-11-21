package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

// Protocol folder actions share the existing one-intent-per-account journal.
// An unresolved barrier survives reopen; no recovery path repeats the write.
func (db *DB) ReconcileProtocolFolders(ctx context.Context, account string, folders []domain.Folder) error {
	if len(folders) > 10000 {
		return errors.New("folder list exceeds limit")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var provider string
		if err := tx.QueryRowContext(ctx, "SELECT provider FROM accounts WHERE id=?", account).Scan(&provider); err != nil {
			return err
		}
		if provider != "imap" && provider != "outlook" {
			return errors.New("provider does not support folder management")
		}
		seen := map[string]bool{}
		ids := map[string]bool{}
		for _, f := range folders {
			if f.ID == "" || ids[f.ID] {
				return errors.New("invalid folder identity")
			}
			ids[f.ID] = true
		}
		for _, f := range folders {
			if f.AccountID != account || f.ID == "" || f.RemoteID == "" || seen[f.RemoteID] {
				return errors.New("invalid folder snapshot")
			}
			seen[f.RemoteID] = true
			var owner string
			err := tx.QueryRowContext(ctx, "SELECT account_id FROM folders WHERE id=?", f.ID).Scan(&owner)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && owner != account {
				return errors.New("folder belongs to another account")
			}
			if f.ParentID != nil && !ids[*f.ParentID] {
				if err := tx.QueryRowContext(ctx, "SELECT account_id FROM folders WHERE id=?", *f.ParentID).Scan(&owner); err != nil || owner != account {
					return errors.New("folder parent belongs to another account or is unavailable")
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO folders(id,account_id,remote_id,name,folder_type,role,parent_id,is_system,sort_order) VALUES(?,?,?,?,?,?,?,?,?)
				ON CONFLICT(id) DO UPDATE SET remote_id=excluded.remote_id,name=excluded.name,role=excluded.role,parent_id=excluded.parent_id,is_system=excluded.is_system,sort_order=excluded.sort_order WHERE folders.account_id=excluded.account_id`, f.ID, account, f.RemoteID, f.Name, f.FolderType, f.Role, f.ParentID, f.IsSystem, f.SortOrder); err != nil {
				return err
			}
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,remote_id FROM folders WHERE account_id=?", account)
		if err != nil {
			return err
		}
		type absent struct{ id, remote string }
		var missing []absent
		for rows.Next() {
			var f absent
			if err := rows.Scan(&f.id, &f.remote); err != nil {
				rows.Close()
				return err
			}
			if !seen[f.remote] {
				missing = append(missing, f)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, f := range missing {
			if _, err := tx.ExecContext(ctx, `UPDATE pending_mail_ops SET status='failed',attempts=?,next_retry_at=NULL,last_error='Destination folder is unavailable; dismiss this operation before continuing.',updated_at=?
				WHERE account_id=? AND status!='done' AND CASE WHEN json_valid(payload_json) THEN EXISTS(SELECT 1 FROM json_each(payload_json,'$.add_labels') WHERE value=?) ELSE 0 END`, MaxPendingMailOpAttempts, time.Now().Unix(), account, f.remote); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM folders WHERE id=? AND account_id=?", f.id, account); err != nil {
				return err
			}
		}
		var intent GmailLabelChange
		err = tx.QueryRowContext(ctx, "SELECT id,action,remote_id,name FROM gmail_label_changes WHERE account_id=?", account).Scan(&intent.ID, &intent.Action, &intent.RemoteID, &intent.Name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		matched := false
		switch intent.Action {
		case "delete":
			matched = !seen[intent.RemoteID]
		case "create":
			count := 0
			for _, f := range folders {
				if !f.IsSystem && f.Name == intent.Name {
					count++
				}
			}
			matched = count == 1
		case "rename":
			for _, f := range folders {
				if !f.IsSystem && f.Name == intent.Name && (f.RemoteID == intent.RemoteID || (!seen[intent.RemoteID] && f.RemoteID == intent.Name)) {
					matched = true
				}
			}
		}
		if matched {
			_, err = tx.ExecContext(ctx, "DELETE FROM gmail_label_changes WHERE account_id=? AND id=?", account, intent.ID)
		}
		return err
	})
}
