package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const MaxPendingMailOpAttempts = 8
const basePendingRetry = time.Minute
const maxPendingRetry = time.Hour

type PendingMailOp struct {
	ID          string  `json:"id"`
	AccountID   string  `json:"account_id"`
	MessageID   string  `json:"message_id"`
	OpType      string  `json:"op_type"`
	Status      string  `json:"status"`
	Result      string  `json:"-"`
	Attempts    int     `json:"attempts"`
	LastError   *string `json:"last_error"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
	NextRetryAt *int64  `json:"next_retry_at"`
}

type PendingMailOpsSummary struct {
	PendingCount     int     `json:"pending_count"`
	InProgressCount  int     `json:"in_progress_count"`
	FailedCount      int     `json:"failed_count"`
	TotalActiveCount int     `json:"total_active_count"`
	LastError        *string `json:"last_error"`
	UpdatedAt        *int64  `json:"updated_at"`
}

type PendingMailOpInput struct {
	AccountID string
	MessageID string
	OpType    string
	Payload   any
}

// result is stored inside payload_json so the legacy v20 schema stays byte
// identical to the Rust oracle. It never leaves the store.
func pendingResult(payload []byte) string {
	var value struct {
		Result string `json:"_result"`
	}
	if json.Unmarshal(payload, &value) == nil && (value.Result == "remote_succeeded" || value.Result == "outcome_unknown") {
		return value.Result
	}
	return "pending"
}

func withResult(payload []byte, result string) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil || value == nil {
		return nil, errors.New("invalid pending operation payload")
	}
	value["_result"] = result
	return json.Marshal(value)
}

func retryDelay(attempts int) time.Duration {
	delay := basePendingRetry
	for i := 1; i < attempts && delay < maxPendingRetry; i++ {
		delay = min(delay*2, maxPendingRetry)
	}
	return delay
}

const pendingColumns = `id,account_id,message_id,op_type,payload_json,status,attempts,last_error,created_at,updated_at,next_retry_at`

func scanPending(row interface{ Scan(...any) error }) (PendingMailOp, error) {
	var op PendingMailOp
	var payload string
	err := row.Scan(&op.ID, &op.AccountID, &op.MessageID, &op.OpType, &payload, &op.Status, &op.Attempts, &op.LastError, &op.CreatedAt, &op.UpdatedAt, &op.NextRetryAt)
	op.Result = pendingResult([]byte(payload))
	return op, err
}

// QueueMessageOps commits pending rows and local cache effects atomically.
func (db *DB) QueueMessageOps(ctx context.Context, ops []PendingMailOpInput, apply func(*sql.Tx) error) error {
	if len(ops) == 0 {
		return errors.New("empty pending operation batch")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		for i, op := range ops {
			if op.AccountID == "" || op.MessageID == "" || op.OpType == "" {
				return errors.New("incomplete pending operation")
			}
			var owner string
			if err := tx.QueryRowContext(ctx, "SELECT account_id FROM messages WHERE id=? AND is_deleted=0", op.MessageID).Scan(&owner); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			if owner != op.AccountID {
				return errors.New("pending operation crosses account boundary")
			}
			payload, err := json.Marshal(op.Payload)
			if err != nil {
				return err
			}
			id := fmt.Sprintf("op-%d-%d-%d", now, i, time.Now().UnixNano()%1000000)
			if _, err := tx.ExecContext(ctx, `INSERT INTO pending_mail_ops(id,account_id,message_id,op_type,payload_json,status,attempts,created_at,updated_at,next_retry_at)
				VALUES(?,?,?,?,?,'pending',0,?,?,NULL)`, id, op.AccountID, op.MessageID, op.OpType, string(payload), now, now); err != nil {
				return err
			}
		}
		if apply != nil {
			return apply(tx)
		}
		return nil
	})
}

func (db *DB) PendingMailOps(ctx context.Context, account string, limit int) ([]PendingMailOp, error) {
	limit = min(max(limit, 1), 500)
	query := "SELECT " + pendingColumns + " FROM pending_mail_ops WHERE status != 'done'"
	args := []any{}
	if account != "" {
		query += " AND account_id=?"
		args = append(args, account)
	}
	query += " ORDER BY updated_at DESC,id LIMIT ?"
	args = append(args, limit)
	rows, err := db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingMailOp{}
	for rows.Next() {
		op, err := scanPending(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

func (db *DB) PendingMailOpsSummary(ctx context.Context, account string) (PendingMailOpsSummary, error) {
	var summary PendingMailOpsSummary
	query := `SELECT COALESCE(SUM(status='pending'),0),COALESCE(SUM(status='in_progress'),0),COALESCE(SUM(status='failed'),0),MAX(updated_at) FROM pending_mail_ops WHERE status != 'done'`
	args := []any{}
	if account != "" {
		query += " AND account_id=?"
		args = append(args, account)
	}
	var updated sql.NullInt64
	if err := db.read.QueryRowContext(ctx, query, args...).Scan(&summary.PendingCount, &summary.InProgressCount, &summary.FailedCount, &updated); err != nil {
		return summary, err
	}
	if updated.Valid {
		summary.UpdatedAt = &updated.Int64
	}
	summary.TotalActiveCount = summary.PendingCount + summary.InProgressCount + summary.FailedCount
	errorQuery := "SELECT last_error FROM pending_mail_ops WHERE status='failed' AND last_error IS NOT NULL"
	errorArgs := []any{}
	if account != "" {
		errorQuery += " AND account_id=?"
		errorArgs = append(errorArgs, account)
	}
	errorQuery += " ORDER BY updated_at DESC LIMIT 1"
	var lastError string
	if err := db.read.QueryRowContext(ctx, errorQuery, errorArgs...).Scan(&lastError); err == nil {
		summary.LastError = &lastError
	} else if !errors.Is(err, sql.ErrNoRows) {
		return summary, err
	}
	return summary, nil
}

// ClaimPendingOps atomically marks due rows in_progress and returns payloads.
func (db *DB) ClaimPendingOps(ctx context.Context, limit int, now int64) ([]PendingMailOp, map[string]json.RawMessage, error) {
	ops := []PendingMailOp{}
	payloads := map[string]json.RawMessage{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+pendingColumns+` FROM pending_mail_ops
			WHERE status='pending' OR (status='failed' AND attempts < ? AND (next_retry_at IS NULL OR next_retry_at <= ?))
			ORDER BY updated_at,id LIMIT ?`, MaxPendingMailOpAttempts, now, min(max(limit, 1), 100))
		if err != nil {
			return err
		}
		rawPayloads := map[string]string{}
		for rows.Next() {
			var op PendingMailOp
			var payload string
			if err := rows.Scan(&op.ID, &op.AccountID, &op.MessageID, &op.OpType, &payload, &op.Status, &op.Attempts, &op.LastError, &op.CreatedAt, &op.UpdatedAt, &op.NextRetryAt); err != nil {
				rows.Close()
				return err
			}
			op.Result = pendingResult([]byte(payload))
			rawPayloads[op.ID] = payload
			ops = append(ops, op)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, op := range ops {
			payloads[op.ID] = json.RawMessage(rawPayloads[op.ID])
			if _, err := tx.ExecContext(ctx, "UPDATE pending_mail_ops SET status='in_progress',updated_at=? WHERE id=?", now, op.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return ops, payloads, err
}

func (db *DB) CompletePendingOp(ctx context.Context, id string) error {
	_, err := db.write.ExecContext(ctx, "UPDATE pending_mail_ops SET status='done',last_error=NULL,next_retry_at=NULL,updated_at=? WHERE id=?", time.Now().Unix(), id)
	return err
}

func (db *DB) FailPendingOp(ctx context.Context, id string, errText string, outcomeUnknown bool) error {
	now := time.Now().Unix()
	var attempts int
	var payload []byte
	if err := db.read.QueryRowContext(ctx, "SELECT attempts,payload_json FROM pending_mail_ops WHERE id=?", id).Scan(&attempts, &payload); err != nil {
		return err
	}
	attempts++
	if outcomeUnknown {
		updated, err := withResult(payload, "outcome_unknown")
		if err != nil {
			return err
		}
		_, err = db.write.ExecContext(ctx, "UPDATE pending_mail_ops SET status='failed',payload_json=?,attempts=?,last_error=?,next_retry_at=NULL,updated_at=? WHERE id=?", string(updated), min(attempts, MaxPendingMailOpAttempts), errText, now, id)
		return err
	}
	if attempts >= MaxPendingMailOpAttempts {
		_, err := db.write.ExecContext(ctx, "UPDATE pending_mail_ops SET status='failed',attempts=?,last_error=?,next_retry_at=NULL,updated_at=? WHERE id=?", attempts, errText, now, id)
		return err
	}
	next := now + int64(retryDelay(attempts)/time.Second)
	_, err := db.write.ExecContext(ctx, "UPDATE pending_mail_ops SET status='failed',attempts=?,last_error=?,next_retry_at=?,updated_at=? WHERE id=?", attempts, errText, next, now, id)
	return err
}

// ResetInterruptedPendingOps replays interrupted writes except unconfirmed
// sends, whose remote outcome cannot be known.
func (db *DB) ResetInterruptedPendingOps(ctx context.Context) error {
	now := time.Now().Unix()
	rows, err := db.read.QueryContext(ctx, "SELECT id,payload_json FROM pending_mail_ops WHERE status='in_progress' AND op_type='send'")
	if err != nil {
		return err
	}
	interrupted := map[string][]byte{}
	for rows.Next() {
		var id string
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return err
		}
		interrupted[id] = payload
	}
	rows.Close()
	for id, payload := range interrupted {
		if pendingResult(payload) == "remote_succeeded" {
			continue
		}
		updated, err := withResult(payload, "outcome_unknown")
		if err != nil {
			return err
		}
		if _, err := db.write.ExecContext(ctx, "UPDATE pending_mail_ops SET status='failed',payload_json=?,attempts=?,last_error=?,next_retry_at=NULL,updated_at=? WHERE id=?", string(updated), MaxPendingMailOpAttempts, "Send outcome is unknown after interruption; check Sent before sending again.", now, id); err != nil {
			return err
		}
	}
	_, err = db.write.ExecContext(ctx, "UPDATE pending_mail_ops SET status='pending',next_retry_at=NULL,updated_at=? WHERE status='in_progress'", now)
	return err
}

// RetryFailedPendingOps makes failed, non-unknown operations due immediately.
func (db *DB) RetryFailedPendingOps(ctx context.Context, account string) (int64, error) {
	query := "UPDATE pending_mail_ops SET next_retry_at=0,updated_at=? WHERE status='failed' AND json_extract(payload_json,'$._result') IS NOT 'outcome_unknown'"
	args := []any{time.Now().Unix()}
	if account != "" {
		query += " AND account_id=?"
		args = append(args, account)
	}
	result, err := db.write.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) DismissFailedPendingOps(ctx context.Context, account string) (int64, error) {
	query := "UPDATE pending_mail_ops SET status='done',last_error=NULL,next_retry_at=NULL,updated_at=? WHERE status='failed'"
	args := []any{time.Now().Unix()}
	if account != "" {
		query += " AND account_id=?"
		args = append(args, account)
	}
	result, err := db.write.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) ActivePendingOpAccounts(ctx context.Context) ([]string, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT DISTINCT account_id FROM pending_mail_ops WHERE status != 'done' ORDER BY account_id")
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
	sort.Strings(out)
	return out, rows.Err()
}
