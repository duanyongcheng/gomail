package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

type MessageFailure struct {
	RemoteID         string
	Kind             string
	NotifyOnRecovery bool
}

func failureDelay(attempts int) time.Duration {
	delay := 5 * time.Minute
	for i := 1; i < attempts && delay < 6*time.Hour; i++ {
		delay = min(delay*2, 6*time.Hour)
	}
	return delay
}

func saveMessageFailures(ctx context.Context, tx *sql.Tx, snapshot Snapshot) error {
	for _, id := range snapshot.RecoveredRemoteIDs {
		if _, err := tx.ExecContext(ctx, "DELETE FROM gmail_message_failures WHERE account_id=? AND remote_id=?", snapshot.AccountID, id); err != nil {
			return err
		}
	}
	for _, failure := range snapshot.Failures {
		if failure.RemoteID == "" || failure.Kind != "too_large" && failure.Kind != "invalid_content" {
			return errors.New("invalid message failure")
		}
		attempts := 0
		err := tx.QueryRowContext(ctx, "SELECT attempts FROM gmail_message_failures WHERE account_id=? AND remote_id=?", snapshot.AccountID, failure.RemoteID).Scan(&attempts)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		attempts = min(attempts+1, 1000000)
		now := time.Now().Unix()
		_, err = tx.ExecContext(ctx, `INSERT INTO gmail_message_failures(account_id,remote_id,kind,attempts,next_retry_at,updated_at,notify_on_recovery)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(account_id,remote_id) DO UPDATE SET kind=excluded.kind,attempts=excluded.attempts,next_retry_at=excluded.next_retry_at,updated_at=excluded.updated_at`, snapshot.AccountID, failure.RemoteID, failure.Kind, attempts, now+int64(failureDelay(attempts)/time.Second), now, failure.NotifyOnRecovery)
		if err != nil {
			return err
		}
	}
	return nil
}

const failureColumns = `f.account_id,f.remote_id,a.email,COALESCE(m.subject,''),f.kind,f.attempts,f.next_retry_at,f.updated_at,f.notify_on_recovery`
const failureFrom = ` FROM gmail_message_failures f JOIN accounts a ON a.id=f.account_id LEFT JOIN messages m ON m.account_id=f.account_id AND m.remote_id=f.remote_id AND m.id=f.account_id || ':' || f.remote_id`

func scanFailure(row interface{ Scan(...any) error }) (domain.DownloadFailure, error) {
	var f domain.DownloadFailure
	err := row.Scan(&f.AccountID, &f.RemoteID, &f.AccountEmail, &f.Subject, &f.Kind, &f.Attempts, &f.NextRetryAt, &f.UpdatedAt, &f.NotifyOnRecovery)
	return f, err
}

func (db *DB) MessageFailure(ctx context.Context, account, id string) (domain.DownloadFailure, error) {
	return scanFailure(db.read.QueryRowContext(ctx, "SELECT "+failureColumns+failureFrom+" WHERE f.account_id=? AND f.remote_id=?", account, id))
}

func (db *DB) ReadyMessageFailures(ctx context.Context, account string, now int64, limit int) ([]domain.DownloadFailure, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT "+failureColumns+failureFrom+" WHERE f.account_id=? AND f.next_retry_at<=? ORDER BY f.next_retry_at,f.remote_id LIMIT ?", account, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DownloadFailure{}
	for rows.Next() {
		f, err := scanFailure(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (db *DB) DownloadFailures(ctx context.Context, account string, limit, offset int) (domain.DownloadFailuresPage, error) {
	limit = min(max(limit, 1), 100)
	offset = max(offset, 0)
	out := domain.DownloadFailuresPage{Items: []domain.DownloadFailure{}}
	where := " WHERE (?='' OR f.account_id=?)"
	if err := db.read.QueryRowContext(ctx, "SELECT count(*) FROM gmail_message_failures f"+where, account, account).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := db.read.QueryContext(ctx, "SELECT "+failureColumns+failureFrom+where+" ORDER BY f.updated_at DESC,f.account_id,f.remote_id LIMIT ? OFFSET ?", account, account, limit, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		f, err := scanFailure(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, f)
	}
	return out, rows.Err()
}

func (db *DB) MessageFailureCounts(ctx context.Context, account string, now int64) (total, due int, err error) {
	err = db.read.QueryRowContext(ctx, "SELECT count(*),COALESCE(sum(next_retry_at<=?),0) FROM gmail_message_failures WHERE account_id=?", now, account).Scan(&total, &due)
	return
}

func (db *DB) MessageFailureAccounts(ctx context.Context, account string) ([]string, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT DISTINCT account_id FROM gmail_message_failures WHERE (?='' OR account_id=?) ORDER BY account_id", account, account)
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

// QueueMessageFailures only changes retry eligibility. It never fetches mail or
// removes a failure before a successful fetch (or confirmed 404) is committed.
func (db *DB) QueueMessageFailures(ctx context.Context, account string) error {
	_, err := db.write.ExecContext(ctx, "UPDATE gmail_message_failures SET next_retry_at=0 WHERE account_id=?", account)
	return err
}
