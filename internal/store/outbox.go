package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bary/pebble-go/internal/domain"
)

const MaxOutboxBytes = 200 << 20
const MaxOutboxAttempts = 8

type OutboxRecord struct {
	ID, AccountID, RequestKey string
	Data                      []byte
	Summary                   []byte
	Status                    string
	Attempts                  int
	Retryable                 bool
	NextRetryAt               *int64
	LastError, RemoteID       string
	CreatedAt, UpdatedAt      int64
}

const outboxColumns = "id,account_id,request_key,data,summary,status,attempts,retryable,next_retry_at,last_error,remote_id,created_at,updated_at"

func scanOutbox(row interface{ Scan(...any) error }) (OutboxRecord, error) {
	var r OutboxRecord
	err := row.Scan(&r.ID, &r.AccountID, &r.RequestKey, &r.Data, &r.Summary, &r.Status, &r.Attempts, &r.Retryable, &r.NextRetryAt, &r.LastError, &r.RemoteID, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}
func (db *DB) Account(ctx context.Context, id string) (domain.Account, error) {
	var a domain.Account
	err := db.read.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE id=?", id).Scan(&a.ID, &a.Email, &a.DisplayName, &a.Color, &a.Provider, &a.AccountLabel, &a.ProviderDisplayName, &a.OAuthSubject, &a.CreatedAt, &a.UpdatedAt, &a.SortOrder)
	return a, err
}
func (db *DB) OutboxByKey(ctx context.Context, account, key string) (OutboxRecord, error) {
	return scanOutbox(db.read.QueryRowContext(ctx, "SELECT "+outboxColumns+" FROM outbox WHERE account_id=? AND request_key=?", account, key))
}
func (db *DB) Outbox(ctx context.Context, account, id string) (OutboxRecord, error) {
	return scanOutbox(db.read.QueryRowContext(ctx, "SELECT "+outboxColumns+" FROM outbox WHERE account_id=? AND id=?", account, id))
}
func (db *DB) QueueOutbox(ctx context.Context, record OutboxRecord) (string, error) {
	id := record.ID
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, "SELECT id FROM outbox WHERE account_id=? AND request_key=?", record.AccountID, record.RequestKey).Scan(&existing)
		if err == nil {
			id = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var size int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(COALESCE(length(data),0)+COALESCE(length(summary),0)),0) FROM outbox").Scan(&size); err != nil {
			return err
		}
		if size+int64(len(record.Data))+int64(len(record.Summary)) > MaxOutboxBytes {
			return errors.New("outbox storage limit reached; clear finished items")
		}
		now := time.Now().Unix()
		_, err = tx.ExecContext(ctx, "INSERT INTO outbox(id,account_id,request_key,data,summary,status,created_at,updated_at) VALUES(?,?,?,?,?,'queued',?,?)", record.ID, record.AccountID, record.RequestKey, record.Data, record.Summary, now, now)
		return err
	})
	return id, err
}
func (db *DB) OutboxList(ctx context.Context, account string) ([]OutboxRecord, error) {
	query := "SELECT id,account_id,request_key,NULL,summary,status,attempts,retryable,next_retry_at,last_error,remote_id,created_at,updated_at FROM outbox WHERE status NOT IN ('cancelled','dismissed')"
	args := []any{}
	if account != "" {
		query += " AND account_id=?"
		args = append(args, account)
	}
	query += " ORDER BY status='sent',created_at DESC,id LIMIT 50"
	rows, err := db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutboxRecord{}
	for rows.Next() {
		r, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (db *DB) ClaimOutbox(ctx context.Context, now int64) (OutboxRecord, error) {
	var record OutboxRecord
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		record, err = scanOutbox(tx.QueryRowContext(ctx, "SELECT "+outboxColumns+" FROM outbox WHERE status='queued' OR (status='failed' AND retryable=1 AND attempts<? AND next_retry_at<=?) ORDER BY created_at,id LIMIT 1", MaxOutboxAttempts, now))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE outbox SET status='preparing',attempts=attempts+1,updated_at=? WHERE id=?", now, record.ID)
		record.Attempts++
		record.Status = "preparing"
		return err
	})
	return record, err
}

// BeginOutboxSend is persisted before the first byte of a send request. Failure
// here means the provider MUST NOT submit; restart quarantines this state.
func (db *DB) BeginOutboxSend(ctx context.Context, id string) error {
	result, err := db.write.ExecContext(ctx, "UPDATE outbox SET status='in_progress',updated_at=? WHERE id=? AND status='preparing'", time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("outbox item no longer available")
	}
	return nil
}
func (db *DB) SettleOutbox(ctx context.Context, id, status, message, remoteID string, retryable bool) error {
	if status != "sent" && status != "failed" && status != "outcome_unknown" {
		return errors.New("invalid outbox result")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var attempts int
		err := tx.QueryRowContext(ctx, "SELECT attempts FROM outbox WHERE id=? AND status IN ('preparing','in_progress')", id).Scan(&attempts)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var next *int64
		if status == "failed" && retryable && attempts < MaxOutboxAttempts {
			value := time.Now().Add(retryDelay(attempts)).Unix()
			next = &value
		}
		_, err = tx.ExecContext(ctx, "UPDATE outbox SET status=?,last_error=?,remote_id=?,retryable=?,next_retry_at=?,updated_at=? WHERE id=?", status, message, remoteID, retryable, next, time.Now().Unix(), id)
		return err
	})
}
func (db *DB) RecoverOutbox(ctx context.Context) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE outbox SET status='outcome_unknown',retryable=0,next_retry_at=NULL,last_error='Send interrupted; check Sent before sending again.',updated_at=? WHERE status='in_progress'", time.Now().Unix()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE outbox SET status='queued',updated_at=? WHERE status='preparing'", time.Now().Unix())
		return err
	})
}
func (db *DB) OutboxAction(ctx context.Context, account, id, action string) error {
	var query string
	switch action {
	case "retry":
		query = "UPDATE outbox SET status='queued',attempts=0,retryable=0,next_retry_at=NULL,last_error='',updated_at=? WHERE account_id=? AND id=? AND status='failed'"
	case "cancel":
		query = "UPDATE outbox SET status='cancelled',data=NULL,summary=NULL,updated_at=? WHERE account_id=? AND id=? AND status IN ('queued','failed')"
	case "dismiss":
		query = "UPDATE outbox SET status='dismissed',data=NULL,summary=NULL,updated_at=? WHERE account_id=? AND id=? AND status IN ('sent','outcome_unknown')"
	default:
		return errors.New("invalid outbox action")
	}
	result, err := db.write.ExecContext(ctx, query, time.Now().Unix(), account, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("outbox state does not allow this action")
	}
	return nil
}
func (db *DB) ConfirmOutboxSent(ctx context.Context, account, id, remote string) error {
	_, err := db.write.ExecContext(ctx, "UPDATE outbox SET status='sent',remote_id=?,last_error='',updated_at=? WHERE account_id=? AND id=? AND status='outcome_unknown'", remote, time.Now().Unix(), account, id)
	return err
}
