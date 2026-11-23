package store

import (
	"context"
	"errors"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
)

func (db *DB) AccountAuthState(ctx context.Context, account string) (domain.ConnectionState, error) {
	var state string
	err := db.read.QueryRowContext(ctx, `SELECT COALESCE(json_extract(COALESCE(NULLIF(sync_state,''),'{}'),'$.gmail_auth_status'),'available') FROM accounts WHERE id=?`, account).Scan(&state)
	if err != nil {
		return "", err
	}
	switch domain.ConnectionState(state) {
	case domain.ConnectionReauthRequired, domain.ConnectionPermissionRequired, domain.ConnectionCredentialsUnavailable, domain.ConnectionConfigurationRequired:
		return domain.ConnectionState(state), nil
	default:
		return domain.ConnectionAvailable, nil
	}
}

// Only terminal access failures are persisted. Transient failures keep their
// existing scheduler/queue retry semantics; checkpoints and other state survive.
func (db *DB) SetAccountAuthState(ctx context.Context, account string, state domain.ConnectionState) error {
	switch state {
	case domain.ConnectionReauthRequired, domain.ConnectionPermissionRequired, domain.ConnectionCredentialsUnavailable, domain.ConnectionConfigurationRequired:
	default:
		return errors.New("invalid persistent account access state")
	}
	_, err := db.write.ExecContext(ctx, `UPDATE accounts SET sync_state=json_set(COALESCE(NULLIF(sync_state,''),'{}'),'$.gmail_auth_status',?) WHERE id=?`, string(state), account)
	return err
}

// ReplaceAccountCredentials cannot recreate an account deleted during OAuth or
// overwrite a newer credential. It changes no mailbox identity or preferences.
func (db *DB) ReplaceAccountCredentials(ctx context.Context, account string, old, next []byte) (bool, error) {
	result, err := db.write.ExecContext(ctx, `UPDATE accounts SET auth_data=?,sync_state=json_remove(COALESCE(NULLIF(sync_state,''),'{}'),'$.gmail_auth_status'),updated_at=? WHERE id=? AND auth_data IS ?`, next, time.Now().Unix(), account, old)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (db *DB) ReleasePendingAccessClaim(ctx context.Context, id string) error {
	_, err := db.write.ExecContext(ctx, `UPDATE pending_mail_ops SET status='pending',last_error='Account access requires attention; queued change is retained.',updated_at=? WHERE id=? AND status='in_progress'`, time.Now().Unix(), id)
	return err
}

func (db *DB) ReleaseOutboxAccessClaim(ctx context.Context, id string) error {
	_, err := db.write.ExecContext(ctx, `UPDATE outbox SET status='queued',attempts=MAX(0,attempts-1),last_error='Account access requires attention; queued message is retained.',updated_at=? WHERE id=? AND status IN ('preparing','in_progress')`, time.Now().Unix(), id)
	return err
}
