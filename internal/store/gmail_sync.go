package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// GmailCheckpoint lives inside accounts.sync_state. HistoryID remains opaque;
// PageTokens detect remote pagination loops, including across process restarts.
type GmailCheckpoint struct {
	Phase                 string   `json:"phase"`
	HistoryID             string   `json:"history_id"`
	PageToken             string   `json:"page_token"`
	PageTokens            []string `json:"page_tokens,omitempty"`
	SuppressNotifications bool     `json:"suppress_notifications,omitempty"`
}

type GmailSyncState struct {
	Cursor     string           `json:"last_sync_cursor"`
	Checkpoint *GmailCheckpoint `json:"gmail_checkpoint"`
}

func (db *DB) GmailState(ctx context.Context, account string) (GmailSyncState, error) {
	var raw sql.NullString
	err := db.read.QueryRowContext(ctx, "SELECT sync_state FROM accounts WHERE id=?", account).Scan(&raw)
	var state GmailSyncState
	if err == nil && raw.Valid && raw.String != "" {
		err = json.Unmarshal([]byte(raw.String), &state)
	}
	return state, err
}

// BackfillPending contains only mail cached before the full scan that has not
// been encountered yet. Absence from a list never establishes remote deletion.
func (db *DB) BackfillPending(ctx context.Context, account string, limit int) ([]string, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT remote_id FROM gmail_backfill_pending WHERE account_id=? ORDER BY remote_id LIMIT ?", account, limit)
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
