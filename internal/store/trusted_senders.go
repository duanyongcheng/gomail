package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func (db *DB) TrustSender(ctx context.Context, account, email, level string) error {
	normalized, err := domain.NormalizeTrustedEmail(email)
	if err != nil {
		return err
	}
	if account == "" || (level != "images" && level != "all") {
		return errors.New("invalid account or trust type")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=?)", account).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("trust account does not exist")
		}
		// Remove legacy spelling/case variants atomically before inserting the
		// explicit chosen level. Rollback retains the previous policy on failure.
		if _, err := tx.ExecContext(ctx, "DELETE FROM trusted_senders WHERE account_id=? AND lower(trim(email))=?", account, normalized); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO trusted_senders(account_id,email,trust_type,created_at) VALUES(?,?,?,?)", account, normalized, level, time.Now().Unix())
		return err
	})
}
func (db *DB) SenderTrust(ctx context.Context, account, email string) (string, error) {
	normalized, err := domain.NormalizeTrustedEmail(email)
	if err != nil {
		return "", nil
	} // Invalid From can never inherit trust.
	rows, err := db.read.QueryContext(ctx, "SELECT trust_type FROM trusted_senders WHERE account_id=? AND lower(trim(email))=?", account, normalized)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	level := ""
	for rows.Next() {
		var candidate string
		if err := rows.Scan(&candidate); err != nil {
			return "", err
		}
		if candidate != "images" && candidate != "all" {
			return "", errors.New("invalid stored trust level")
		}
		if level == "" || candidate == "images" {
			level = candidate
		}
	}
	return level, rows.Err()
}
func (db *DB) TrustedSenders(ctx context.Context, account string) ([]domain.TrustedSender, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT account_id,email,trust_type,created_at FROM trusted_senders WHERE account_id=? ORDER BY created_at,email", account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byEmail := map[string]domain.TrustedSender{}
	for rows.Next() {
		var sender domain.TrustedSender
		if err := rows.Scan(&sender.AccountID, &sender.Email, &sender.TrustType, &sender.CreatedAt); err != nil {
			return nil, err
		}
		normalized, err := domain.NormalizeTrustedEmail(sender.Email)
		if err != nil {
			continue
		} // Invalid imported records never grant trust.
		sender.Email = normalized
		if sender.TrustType != "images" && sender.TrustType != "all" {
			return nil, errors.New("invalid stored trust level")
		}
		if previous, ok := byEmail[normalized]; ok {
			if previous.TrustType == "images" {
				sender.TrustType = "images"
			}
			sender.CreatedAt = min(sender.CreatedAt, previous.CreatedAt)
		}
		byEmail[normalized] = sender
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.TrustedSender, 0, len(byEmail))
	for _, sender := range byEmail {
		out = append(out, sender)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}
func (db *DB) RemoveTrustedSender(ctx context.Context, account, email string) error {
	normalized, err := domain.NormalizeTrustedEmail(email)
	if err != nil {
		return err
	}
	if account == "" {
		return errors.New("trust account required")
	}
	_, err = db.write.ExecContext(ctx, "DELETE FROM trusted_senders WHERE account_id=? AND lower(trim(email))=?", account, normalized)
	return err
}
