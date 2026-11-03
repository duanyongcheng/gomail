package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/bary/pebble-go/internal/domain"
)

// MailOpPayload is the persisted, provider-neutral label intent. The same
// payload drives the immediate cache effect, sync overlay and remote replay.
type MailOpPayload struct {
	RemoteID       string   `json:"remote_id"`
	AddLabels      []string `json:"add_labels,omitempty"`
	RemoveLabels   []string `json:"remove_labels,omitempty"`
	IsRead         *bool    `json:"is_read,omitempty"`
	IsStarred      *bool    `json:"is_starred,omitempty"`
	TargetRemoteID string   `json:"target_remote_id,omitempty"`
}

type MailMutation struct {
	MessageIDs     []string
	AccountID      string
	FolderIDs      []string
	AllUnread      bool
	OpType         string
	ToggleArchive  bool
	TargetFolderID string
	IsRead         *bool
	IsStarred      *bool
}

type MailMutationResult struct {
	Count         int
	ArchiveResult string
}

// QueueMailMutation owns the entire read/decide/queue/apply transaction. In
// particular concurrent archive toggles must not decide from the same old state.
func (db *DB) QueueMailMutation(ctx context.Context, request MailMutation) (MailMutationResult, error) {
	if request.AllUnread && request.AccountID == "" {
		return MailMutationResult{}, errors.New("account required")
	}
	var result MailMutationResult
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		ids := request.MessageIDs
		if request.AllUnread {
			for _, folder := range request.FolderIDs {
				var owner string
				if err := tx.QueryRowContext(ctx, "SELECT account_id FROM folders WHERE id=?", folder).Scan(&owner); err != nil {
					return err
				}
				if owner != request.AccountID {
					return errors.New("folder belongs to another account")
				}
			}
			query := "SELECT id FROM messages WHERE account_id=? AND is_read=0 AND is_deleted=0"
			args := []any{request.AccountID}
			if len(request.FolderIDs) > 0 {
				query += " AND EXISTS(SELECT 1 FROM message_folders WHERE message_id=messages.id AND folder_id IN (" + placeholders(len(request.FolderIDs)) + "))"
				for _, id := range request.FolderIDs {
					args = append(args, id)
				}
			}
			rows, err := tx.QueryContext(ctx, query+" ORDER BY id", args...)
			if err != nil {
				return err
			}
			ids = nil
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			m, err := scanMessage(tx.QueryRowContext(ctx, "SELECT "+messageColumns+" FROM messages m WHERE id=? AND is_deleted=0", id))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if request.AccountID != "" && request.AccountID != m.AccountID {
				return errors.New("message belongs to another account")
			}
			var provider string
			if err := tx.QueryRowContext(ctx, "SELECT provider FROM accounts WHERE id=?", m.AccountID).Scan(&provider); err != nil {
				return err
			}
			if provider == "pop3" {
				return errors.New("POP3 does not support remote flags or folder changes")
			}
			payload := MailOpPayload{RemoteID: m.RemoteID}
			opType := request.OpType
			switch opType {
			case "update_flags":
				payload.IsRead, payload.IsStarred = request.IsRead, request.IsStarred
				if request.IsRead == nil && request.IsStarred == nil {
					continue
				}
				if request.IsRead != nil {
					if *request.IsRead {
						payload.RemoveLabels = append(payload.RemoveLabels, "UNREAD")
					} else {
						payload.AddLabels = append(payload.AddLabels, "UNREAD")
					}
				}
				if request.IsStarred != nil {
					if *request.IsStarred {
						payload.AddLabels = append(payload.AddLabels, "STARRED")
					} else {
						payload.RemoveLabels = append(payload.RemoveLabels, "STARRED")
					}
				}
			case "archive":
				inInbox := true
				if request.ToggleArchive {
					if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM message_folders mf JOIN folders f ON f.id=mf.folder_id WHERE mf.message_id=? AND f.account_id=? AND (f.remote_id='INBOX' OR f.role='inbox'))`, id, m.AccountID).Scan(&inInbox); err != nil {
						return err
					}
				}
				if inInbox {
					payload.RemoveLabels = []string{"INBOX"}
					result.ArchiveResult = "archived"
				} else {
					opType = "restore"
					payload.AddLabels, payload.RemoveLabels = []string{"INBOX"}, []string{"TRASH", "SPAM"}
					result.ArchiveResult = "unarchived"
				}
			case "restore":
				payload.AddLabels, payload.RemoveLabels = []string{"INBOX"}, []string{"TRASH", "SPAM"}
			case "delete":
				payload.AddLabels, payload.RemoveLabels = []string{"TRASH"}, []string{"INBOX"}
			case "move_to_folder":
				var owner, remote string
				if err := tx.QueryRowContext(ctx, "SELECT account_id,remote_id FROM folders WHERE id=?", request.TargetFolderID).Scan(&owner, &remote); err != nil {
					return err
				}
				if owner != m.AccountID {
					return errors.New("target folder belongs to another account")
				}
				var deleting bool
				if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM gmail_label_changes WHERE account_id=? AND action='delete' AND remote_id=?)", m.AccountID, remote).Scan(&deleting); err != nil {
					return err
				}
				if deleting {
					return errors.New("target label is being deleted; refresh Gmail labels first")
				}
				payload.AddLabels = []string{remote}
				if remote == "INBOX" {
					payload.RemoveLabels = []string{"TRASH", "SPAM"}
				} else {
					payload.RemoveLabels = []string{"INBOX"}
				}
			default:
				return errors.New("unsupported mail mutation")
			}
			if provider != "gmail" && opType != "update_flags" {
				if err := protocolFolderIntent(ctx, tx, m, opType, request.TargetFolderID, &payload); err != nil {
					return err
				}
			}
			op := PendingMailOpInput{AccountID: m.AccountID, MessageID: id, OpType: opType, Payload: payload}
			if err := insertPendingOp(ctx, tx, op); err != nil {
				return err
			}
			if err := applyLabelIntent(ctx, tx, m.AccountID, id, payload); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE messages SET updated_at=? WHERE id=?", time.Now().Unix(), id); err != nil {
				return err
			}
			result.Count++
		}
		return nil
	})
	if err != nil {
		return MailMutationResult{}, err
	}
	return result, nil
}

// Protocol mailboxes move messages between real remote folders. Gmail's virtual
// label names must never be sent to an unrelated server or synthesized locally.
func protocolFolderIntent(ctx context.Context, tx *sql.Tx, m domain.Message, operation, targetID string, payload *MailOpPayload) error {
	var target string
	if operation == "move_to_folder" {
		if err := tx.QueryRowContext(ctx, "SELECT remote_id FROM folders WHERE id=? AND account_id=?", targetID, m.AccountID).Scan(&target); err != nil {
			return err
		}
	} else {
		role := map[string]string{"archive": "archive", "delete": "trash", "restore": "inbox"}[operation]
		if role == "" {
			return errors.New("unsupported protocol folder operation")
		}
		if err := tx.QueryRowContext(ctx, "SELECT remote_id FROM folders WHERE account_id=? AND role=? ORDER BY id LIMIT 1", m.AccountID, role).Scan(&target); err != nil {
			return errors.New("required destination folder is unavailable; refresh folders first")
		}
	}
	payload.TargetRemoteID = target
	payload.AddLabels = []string{target}
	payload.RemoveLabels = nil
	rows, err := tx.QueryContext(ctx, "SELECT f.remote_id FROM folders f JOIN message_folders mf ON mf.folder_id=f.id WHERE mf.message_id=? AND f.account_id=? AND f.remote_id!=?", m.ID, m.AccountID, target)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var remote string
		if err := rows.Scan(&remote); err != nil {
			return err
		}
		payload.RemoveLabels = append(payload.RemoveLabels, remote)
	}
	return rows.Err()
}

func labelMutation(opType string) bool {
	switch opType {
	case "update_flags", "archive", "unarchive", "restore", "delete", "move_to_folder":
		return true
	}
	return false
}

func applyLabelIntent(ctx context.Context, tx *sql.Tx, account, message string, p MailOpPayload) error {
	var provider string
	if err := tx.QueryRowContext(ctx, "SELECT provider FROM accounts WHERE id=?", account).Scan(&provider); err != nil {
		return err
	}
	for _, change := range []struct {
		labels []string
		add    bool
	}{{p.RemoveLabels, false}, {p.AddLabels, true}} {
		for _, remote := range change.labels {
			if remote == "UNREAD" || remote == "STARRED" {
				column, value := "is_read", !change.add
				if remote == "STARRED" {
					column, value = "is_starred", change.add
				}
				if _, err := tx.ExecContext(ctx, "UPDATE messages SET "+column+"=? WHERE id=? AND account_id=?", value, message, account); err != nil {
					return err
				}
			}
			if !change.add {
				if _, err := tx.ExecContext(ctx, `DELETE FROM message_folders WHERE message_id=? AND folder_id IN (SELECT id FROM folders WHERE account_id=? AND remote_id=?)`, message, account, remote); err != nil {
					return err
				}
				continue
			}
			// Older/incomplete label snapshots may omit system folders. Materialize
			// those only; arbitrary custom folder ownership is never synthesized.
			if provider == "gmail" && (remote == "INBOX" || remote == "TRASH" || remote == "SPAM" || remote == "UNREAD" || remote == "STARRED") {
				var role *string
				if remote != "UNREAD" && remote != "STARRED" {
					value := strings.ToLower(remote)
					role = &value
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO folders(id,account_id,remote_id,name,folder_type,role,is_system)
					SELECT ?,?,?,?,'label',?,1 WHERE NOT EXISTS(SELECT 1 FROM folders WHERE account_id=? AND remote_id=?)`, ScopedID(account, "label:"+remote), account, remote, remote, role, account, remote); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO message_folders(message_id,folder_id) SELECT ?,id FROM folders WHERE account_id=? AND remote_id=?`, message, account, remote); err != nil {
				return err
			}
		}
	}
	return nil
}

// PendingOpWatermark must be captured before remote reads, under the provider's
// sync/write gate. A commit of data fetched earlier cannot release a later write.
func (db *DB) PendingOpWatermark(ctx context.Context) (int64, error) {
	var sequence int64
	err := db.read.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence),0) FROM mail_op_sequence").Scan(&sequence)
	return sequence, err
}

func overlayMailIntents(ctx context.Context, tx *sql.Tx, m domain.Message, watermark *int64) error {
	if watermark != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM mail_op_sequence WHERE kind='succeeded' AND sequence<=?
			AND op_id IN (SELECT id FROM pending_mail_ops WHERE account_id=? AND message_id=? AND status='done')`, *watermark, m.AccountID, m.ID); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.op_type,p.payload_json FROM pending_mail_ops p
		JOIN mail_op_sequence q ON q.op_id=p.id AND q.kind='queued'
		WHERE p.account_id=? AND p.message_id=? AND (p.status!='done' OR EXISTS(SELECT 1 FROM mail_op_sequence s WHERE s.op_id=p.id AND s.kind='succeeded'))
		ORDER BY q.sequence`, m.AccountID, m.ID)
	if err != nil {
		return err
	}
	type intent struct {
		kind    string
		payload []byte
	}
	var intents []intent
	for rows.Next() {
		var i intent
		if err := rows.Scan(&i.kind, &i.payload); err != nil {
			rows.Close()
			return err
		}
		intents = append(intents, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range intents {
		if !labelMutation(i.kind) {
			continue
		}
		var payload MailOpPayload
		// Invalid legacy payloads remain visible for worker error handling; they
		// must not prevent the mailbox from syncing.
		if json.Unmarshal(i.payload, &payload) != nil || payload.RemoteID != m.RemoteID {
			continue
		}
		if err := applyLabelIntent(ctx, tx, m.AccountID, m.ID, payload); err != nil {
			return err
		}
	}
	return nil
}
