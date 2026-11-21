package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

type ProviderCheckpoint struct {
	Provider, Scope string
	Data            []byte
	Delete          bool
}
type ProviderMapping struct {
	Provider, Scope, RemoteID, MessageID string
	Delete                               bool
}
type RemovedMembership struct{ MessageID, FolderID string }
type ProviderMappingScope struct{ Provider, Scope string }

func (db *DB) ProviderCheckpoint(ctx context.Context, account, provider, scope string) ([]byte, error) {
	var data []byte
	err := db.read.QueryRowContext(ctx, `SELECT data FROM provider_checkpoints WHERE account_id=? AND provider=? AND scope=?`, account, provider, scope).Scan(&data)
	return data, err
}
func (db *DB) ProviderMappings(ctx context.Context, account, provider, scope string) ([]ProviderMapping, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT remote_id,message_id FROM provider_mappings WHERE account_id=? AND provider=? AND scope=?`, account, provider, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderMapping{}
	for rows.Next() {
		m := ProviderMapping{Provider: provider, Scope: scope}
		if err := rows.Scan(&m.RemoteID, &m.MessageID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (db *DB) ProviderMappingsPage(ctx context.Context, account, provider, scope, after string, limit int) ([]ProviderMapping, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("invalid mapping page limit")
	}
	rows, err := db.read.QueryContext(ctx, `SELECT remote_id,message_id FROM provider_mappings WHERE account_id=? AND provider=? AND scope=? AND remote_id>? ORDER BY remote_id LIMIT ?`, account, provider, scope, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderMapping{}
	for rows.Next() {
		m := ProviderMapping{Provider: provider, Scope: scope}
		if err := rows.Scan(&m.RemoteID, &m.MessageID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (db *DB) NetworkSettings(ctx context.Context) ([]byte, error) {
	var data []byte
	err := db.read.QueryRowContext(ctx, `SELECT data FROM network_settings WHERE id=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}
func (db *DB) SaveNetworkSettings(ctx context.Context, data []byte) error {
	if len(data) > 16<<10 {
		return errors.New("network settings too large")
	}
	_, err := db.write.ExecContext(ctx, `INSERT INTO network_settings(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, data)
	return err
}
func saveProviderState(ctx context.Context, tx *sql.Tx, s Snapshot) error {
	for _, scope := range s.ProviderMappingScopesCleared {
		if scope.Provider == "" || scope.Scope == "" {
			return errors.New("invalid mapping scope")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM provider_mappings WHERE account_id=? AND provider=? AND scope=?`, s.AccountID, scope.Provider, scope.Scope); err != nil {
			return err
		}
	}
	for _, id := range s.RemovedFolderIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM folders WHERE account_id=? AND id=?`, s.AccountID, id); err != nil {
			return err
		}
	}
	for _, m := range s.RemovedMemberships {
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_folders WHERE message_id=? AND folder_id=? AND message_id IN (SELECT id FROM messages WHERE account_id=?) AND folder_id IN (SELECT id FROM folders WHERE account_id=?)`, m.MessageID, m.FolderID, s.AccountID, s.AccountID); err != nil {
			return err
		}
		// Folder-scoped absence cannot retire account-global local intent or a
		// successful write receipt. Only an authoritative message read does that.
		message, err := scanMessage(tx.QueryRowContext(ctx, "SELECT "+messageColumns+" FROM messages m WHERE id=? AND account_id=? AND is_deleted=0", m.MessageID, s.AccountID))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := overlayMailIntents(ctx, tx, message, nil); err != nil {
			return err
		}
	}
	for _, c := range s.ProviderCheckpoints {
		if c.Provider == "" || c.Scope == "" || len(c.Data) > 1<<20 {
			return errors.New("invalid provider checkpoint")
		}
		if c.Delete {
			if _, err := tx.ExecContext(ctx, `DELETE FROM provider_checkpoints WHERE account_id=? AND provider=? AND scope=?`, s.AccountID, c.Provider, c.Scope); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_checkpoints(account_id,provider,scope,data) VALUES(?,?,?,?) ON CONFLICT(account_id,provider,scope) DO UPDATE SET data=excluded.data`, s.AccountID, c.Provider, c.Scope, c.Data); err != nil {
			return err
		}
	}
	for _, m := range s.ProviderMappings {
		if m.Provider == "" || m.Scope == "" || m.RemoteID == "" {
			return errors.New("invalid provider mapping")
		}
		if m.Delete {
			if _, err := tx.ExecContext(ctx, `DELETE FROM provider_mappings WHERE account_id=? AND provider=? AND scope=? AND remote_id=?`, s.AccountID, m.Provider, m.Scope, m.RemoteID); err != nil {
				return err
			}
			continue
		}
		var owner string
		if err := tx.QueryRowContext(ctx, `SELECT account_id FROM messages WHERE id=?`, m.MessageID).Scan(&owner); err != nil {
			return err
		}
		if owner != s.AccountID {
			return errors.New("provider mapping account mismatch")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_mappings(account_id,provider,scope,remote_id,message_id) VALUES(?,?,?,?,?) ON CONFLICT(account_id,provider,scope,remote_id) DO UPDATE SET message_id=excluded.message_id`, s.AccountID, m.Provider, m.Scope, m.RemoteID, m.MessageID); err != nil {
			return err
		}
	}
	return nil
}

// RemoteIDsPage bounds reconciliation memory; prefix matching is literal, not LIKE syntax.
func (db *DB) RemoteIDsPage(ctx context.Context, account, prefix, after string, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("invalid remote ID page limit")
	}
	rows, err := db.read.QueryContext(ctx, `SELECT remote_id FROM messages WHERE account_id=? AND is_deleted=0 AND substr(remote_id,1,length(?))=? AND remote_id>? ORDER BY remote_id LIMIT ?`, account, prefix, prefix, after, limit)
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

// CreateProtocolAccount deliberately has no upsert: duplicate identities cannot replace credentials.
func (db *DB) CreateProtocolAccount(ctx context.Context, a domain.Account, encrypted []byte) error {
	if len(encrypted) == 0 {
		return errors.New("encrypted configuration required")
	}
	_, err := db.write.ExecContext(ctx, `INSERT INTO accounts(id,email,display_name,provider,auth_data,created_at,updated_at,account_label,color,sort_order) VALUES(?,?,?,?,?,?,?,?,?,(SELECT COALESCE(MAX(sort_order),-1)+1 FROM accounts))`, a.ID, a.Email, a.DisplayName, a.Provider, encrypted, a.CreatedAt, a.UpdatedAt, a.AccountLabel, a.Color)
	return err
}
func (db *DB) ReplaceProtocolAccount(ctx context.Context, a domain.Account, old, next []byte) (bool, error) {
	if len(next) == 0 {
		return false, errors.New("encrypted configuration required")
	}
	result, err := db.write.ExecContext(ctx, `UPDATE accounts SET auth_data=?,display_name=?,account_label=?,sync_state=json_remove(COALESCE(NULLIF(sync_state,''),'{}'),'$.gmail_auth_status','$.provider_send_auth_status','$.provider_write_auth_status'),updated_at=? WHERE id=? AND provider=? AND lower(trim(email))=lower(trim(?)) AND auth_data IS ?`, next, a.DisplayName, a.AccountLabel, time.Now().Unix(), a.ID, a.Provider, a.Email, old)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
