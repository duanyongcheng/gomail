package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/bary/pebble-go/internal/mailnet"
)

func (db *DB) AccountNetwork(ctx context.Context, account string) (mailnet.ProxySetting, error) {
	var raw string
	err := db.read.QueryRowContext(ctx, `SELECT COALESCE(json_extract(COALESCE(NULLIF(sync_state,''),'{}'),'$.account_proxy'),'{}') FROM accounts WHERE id=? AND provider='gmail'`, account).Scan(&raw)
	var setting mailnet.ProxySetting
	if err != nil {
		return setting, err
	}
	if json.Unmarshal([]byte(raw), &setting) != nil {
		return setting, errors.New("Invalid account network settings")
	}
	if _, err := mailnet.ResolveProxy(nil, setting); err != nil {
		return setting, err
	}
	if setting.Mode == "" {
		setting.Mode = "inherit"
	}
	return setting, nil
}
func (db *DB) SetAccountNetwork(ctx context.Context, account string, setting mailnet.ProxySetting) error {
	if _, err := mailnet.ResolveProxy(nil, setting); err != nil {
		return err
	}
	data, err := json.Marshal(setting)
	if err != nil {
		return err
	}
	result, err := db.write.ExecContext(ctx, `UPDATE accounts SET sync_state=json_set(COALESCE(NULLIF(sync_state,''),'{}'),'$.account_proxy',json(?)) WHERE id=? AND provider='gmail'`, string(data), account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return sql.ErrNoRows
	}
	return err
}
