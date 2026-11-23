package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
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
		for _, op := range ops {
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
			if err := insertPendingOp(ctx, tx, op); err != nil {
				return err
			}
		}
		if apply != nil {
			return apply(tx)
		}
		return nil
	})
}

func insertPendingOp(ctx context.Context, tx *sql.Tx, op PendingMailOpInput) error {
	payload, err := json.Marshal(op.Payload)
	if err != nil {
		return err
	}
	id := "op-" + rand.Text()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO pending_mail_ops(id,account_id,message_id,op_type,payload_json,status,attempts,created_at,updated_at,next_retry_at)
		VALUES(?,?,?,?,?,'pending',0,?,?,NULL)`, id, op.AccountID, op.MessageID, op.OpType, string(payload), now, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO mail_op_sequence(op_id,kind) VALUES(?,'queued')", id)
	return err
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
func (db *DB) ClaimPendingOps(ctx context.Context, limit int, now int64, excludedAccounts ...string) ([]PendingMailOp, map[string]json.RawMessage, error) {
	ops := []PendingMailOp{}
	payloads := map[string]json.RawMessage{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		query := `SELECT ` + pendingColumns + ` FROM pending_mail_ops
			WHERE id IN (SELECT p.id FROM pending_mail_ops p
			JOIN mail_op_sequence q ON q.op_id=p.id AND q.kind='queued'
			WHERE (p.status='pending' OR (p.status='failed' AND p.attempts < ? AND (p.next_retry_at IS NULL OR p.next_retry_at <= ?)))
			AND CASE WHEN json_valid(p.payload_json) THEN COALESCE(json_extract(p.payload_json,'$._result'),'pending') ELSE 'pending' END NOT IN ('outcome_unknown','remote_succeeded')
			AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=p.account_id AND COALESCE(json_extract(COALESCE(NULLIF(a.sync_state,''),'{}'),'$.provider_write_auth_status'),'available')!='available')
			AND NOT EXISTS (SELECT 1 FROM pending_mail_ops earlier
				JOIN mail_op_sequence eq ON eq.op_id=earlier.id AND eq.kind='queued'
				WHERE earlier.account_id=p.account_id AND earlier.message_id=p.message_id AND earlier.status!='done' AND eq.sequence<q.sequence))`
		args := []any{MaxPendingMailOpAttempts, now}
		if len(excludedAccounts) > 0 {
			query += " AND account_id NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(excludedAccounts)), ",") + ")"
			for _, account := range excludedAccounts {
				args = append(args, account)
			}
		}
		query += ` ORDER BY (SELECT sequence FROM mail_op_sequence WHERE op_id=pending_mail_ops.id AND kind='queued') LIMIT ?`
		args = append(args, min(max(limit, 1), 100))
		rows, err := tx.QueryContext(ctx, query, args...)
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
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var payload []byte
		if err := tx.QueryRowContext(ctx, "SELECT payload_json FROM pending_mail_ops WHERE id=? AND status='in_progress'", id).Scan(&payload); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		updated, err := withResult(payload, "remote_succeeded")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO mail_op_sequence(op_id,kind) VALUES(?,'succeeded')", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE pending_mail_ops SET status='done',payload_json=?,last_error=NULL,next_retry_at=NULL,updated_at=? WHERE id=?", string(updated), time.Now().Unix(), id)
		return err
	})
}

func (db *DB) FailPendingOp(ctx context.Context, id string, errText string, outcomeUnknown bool) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		var attempts int
		var payload []byte
		if err := tx.QueryRowContext(ctx, "SELECT attempts,payload_json FROM pending_mail_ops WHERE id=? AND status='in_progress'", id).Scan(&attempts, &payload); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		attempts++
		var next *int64
		if outcomeUnknown {
			updated, err := withResult(payload, "outcome_unknown")
			if err != nil {
				return err
			}
			payload = updated
		} else if attempts < MaxPendingMailOpAttempts {
			value := now + int64(retryDelay(attempts)/time.Second)
			next = &value
		}
		_, err := tx.ExecContext(ctx, "UPDATE pending_mail_ops SET status='failed',payload_json=?,attempts=?,last_error=?,next_retry_at=?,updated_at=? WHERE id=?", string(payload), attempts, errText, next, now, id)
		return err
	})
}

// ResetInterruptedPendingOps replays interrupted writes except unconfirmed
// sends, whose remote outcome cannot be known.
func (db *DB) ResetInterruptedPendingOps(ctx context.Context) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		rows, err := tx.QueryContext(ctx, "SELECT p.id,p.op_type,p.status,p.payload_json,a.provider FROM pending_mail_ops p JOIN accounts a ON a.id=p.account_id WHERE p.status!='done'")
		if err != nil {
			return err
		}
		type interruptedOp struct {
			id, kind, status, provider string
			payload                    []byte
		}
		var interrupted []interruptedOp
		for rows.Next() {
			var op interruptedOp
			if err := rows.Scan(&op.id, &op.kind, &op.status, &op.payload, &op.provider); err != nil {
				rows.Close()
				return err
			}
			interrupted = append(interrupted, op)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, op := range interrupted {
			result := pendingResult(op.payload)
			if result == "remote_succeeded" {
				if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO mail_op_sequence(op_id,kind) VALUES(?,'succeeded')", op.id); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, "UPDATE pending_mail_ops SET status='done',last_error=NULL,next_retry_at=NULL,updated_at=? WHERE id=?", now, op.id); err != nil {
					return err
				}
				continue
			}
			nonIdempotent := op.kind == "send" || (op.provider != "gmail" && op.kind != "update_flags")
			if result == "outcome_unknown" || nonIdempotent && op.status == "in_progress" {
				payload := op.payload
				if result != "outcome_unknown" {
					payload, err = withResult(payload, "outcome_unknown")
					if err != nil {
						return err
					}
				}
				query := "UPDATE pending_mail_ops SET status='failed',payload_json=?,last_error=?,next_retry_at=NULL,updated_at=? WHERE id=?"
				if result != "outcome_unknown" {
					query += " AND status='in_progress'"
				}
				if _, err := tx.ExecContext(ctx, query, string(payload), "Remote write outcome is unknown; check the mailbox before another attempt.", now, op.id); err != nil {
					return err
				}
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE pending_mail_ops SET status='pending',next_retry_at=NULL,updated_at=? WHERE status='in_progress'", now); err != nil {
			return err
		}
		// Repair local effects of v23 queued folder writes even when this
		// profile starts offline/manual and no sync will run to apply overlays.
		rows, err = tx.QueryContext(ctx, `SELECT m.id,m.account_id,m.remote_id FROM messages m WHERE m.is_deleted=0
			AND EXISTS(SELECT 1 FROM pending_mail_ops p WHERE p.message_id=m.id AND p.account_id=m.account_id
			AND (p.status!='done' OR EXISTS(SELECT 1 FROM mail_op_sequence s WHERE s.op_id=p.id AND s.kind='succeeded')))`)
		if err != nil {
			return err
		}
		var messages []domain.Message
		for rows.Next() {
			var m domain.Message
			if err := rows.Scan(&m.ID, &m.AccountID, &m.RemoteID); err != nil {
				rows.Close()
				return err
			}
			messages = append(messages, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, m := range messages {
			if err := overlayMailIntents(ctx, tx, m, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

// RetryFailedPendingOps makes failed, non-unknown operations due immediately.
func (db *DB) RetryFailedPendingOps(ctx context.Context, account string) (int64, error) {
	query := `UPDATE pending_mail_ops SET next_retry_at=0,updated_at=? WHERE status='failed' AND attempts<?
		AND CASE WHEN json_valid(payload_json) THEN COALESCE(json_extract(payload_json,'$._result'),'pending') ELSE 'pending' END NOT IN ('outcome_unknown','remote_succeeded')`
	args := []any{time.Now().Unix(), MaxPendingMailOpAttempts}
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
	var count int64
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		query := "SELECT id FROM pending_mail_ops WHERE status='failed'"
		args := []any{}
		if account != "" {
			query += " AND account_id=?"
			args = append(args, account)
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		var ids []string
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
		for _, id := range ids {
			// Do not let later startup interpret a dismissed marker as a fresh
			// remote success. Dismiss also removes any stale overlay receipt.
			if _, err := tx.ExecContext(ctx, "DELETE FROM mail_op_sequence WHERE op_id=? AND kind='succeeded'", id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE pending_mail_ops SET status='done',last_error=NULL,next_retry_at=NULL,updated_at=? WHERE id=?", time.Now().Unix(), id); err != nil {
				return err
			}
		}
		count = int64(len(ids))
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
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
