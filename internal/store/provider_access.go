package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
)

func (db *DB) ProviderSendState(ctx context.Context, account string) (domain.ConnectionState, error) {
	var state string
	err := db.read.QueryRowContext(ctx, `SELECT COALESCE(json_extract(COALESCE(NULLIF(sync_state,''),'{}'),'$.provider_send_auth_status'),'available') FROM accounts WHERE id=?`, account).Scan(&state)
	return domain.ConnectionState(state), err
}

func (db *DB) ProviderWriteState(ctx context.Context, account string) (domain.ConnectionState, error) {
	var state string
	err := db.read.QueryRowContext(ctx, `SELECT COALESCE(json_extract(COALESCE(NULLIF(sync_state,''),'{}'),'$.provider_write_auth_status'),'available') FROM accounts WHERE id=?`, account).Scan(&state)
	return domain.ConnectionState(state), err
}

// Pause only the submitting resource; healthy reads and unrelated resource grants
// remain usable. Credential replacement clears this status, never unknown sends.
func (db *DB) PauseOutboxResource(ctx context.Context, id string, state domain.ConnectionState) error {
	if state == "" || !state.Blocked() {
		return errors.New("terminal resource access state required")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var account string
		if err := tx.QueryRowContext(ctx, "SELECT account_id FROM outbox WHERE id=? AND status IN ('preparing','in_progress')", id).Scan(&account); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET sync_state=json_set(COALESCE(NULLIF(sync_state,''),'{}'),'$.provider_send_auth_status',?) WHERE id=?`, string(state), account); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE outbox SET status='queued',attempts=MAX(0,attempts-1),last_error='Sending authorization requires attention; queued message is retained.',updated_at=? WHERE id=? AND status IN ('preparing','in_progress')`, time.Now().Unix(), id)
		return err
	})
}

func (db *DB) PausePendingResource(ctx context.Context, id string, state domain.ConnectionState) error {
	if state == "" || !state.Blocked() {
		return errors.New("terminal resource access state required")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var account string
		if err := tx.QueryRowContext(ctx, "SELECT account_id FROM pending_mail_ops WHERE id=? AND status='in_progress'", id).Scan(&account); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET sync_state=json_set(COALESCE(NULLIF(sync_state,''),'{}'),'$.provider_write_auth_status',?) WHERE id=?`, string(state), account); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE pending_mail_ops SET status='pending',last_error='Mail modification permission requires attention; queued change is retained.',updated_at=? WHERE id=? AND status='in_progress'`, time.Now().Unix(), id)
		return err
	})
}
