package store

import (
	"context"
	"encoding/json"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailnet"
)

// CreateGmailAccount pins the authorization route with the initial credentials.
// A concurrent creation must never replace an existing mailbox or its route.
func (db *DB) CreateGmailAccount(ctx context.Context, a domain.Account, encrypted []byte, route mailnet.ProxySetting) error {
	if _, err := mailnet.ResolveProxy(nil, route); err != nil {
		return err
	}
	state, err := json.Marshal(map[string]any{"account_proxy": route})
	if err != nil {
		return err
	}
	_, err = db.write.ExecContext(ctx, `INSERT INTO accounts(id,email,display_name,color,provider,account_label,created_at,updated_at,auth_data,sync_state,sort_order)
 VALUES(?,?,?,?,?,?,?,?,?,?,(SELECT COALESCE(MAX(sort_order),-1)+1 FROM accounts))`, a.ID, a.Email, a.DisplayName, a.Color, a.Provider, a.AccountLabel, a.CreatedAt, a.UpdatedAt, encrypted, string(state))
	return err
}
