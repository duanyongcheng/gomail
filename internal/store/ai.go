package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	maxAISettingsBytes = 64 << 10
	maxAICacheBytes    = 2 << 20
)

type AICacheEntry struct {
	TargetID    string
	AccountID   string
	Kind        string // "summary" | "thread_summary" | "translation"
	TargetLang  string
	ContentHash string
	Model       string
	Payload     string
	CreatedAt   int64
	UpdatedAt   int64
}

type AICacheStats struct {
	SummaryCount     int
	TranslationCount int
}

func (db *DB) AISettings(ctx context.Context) ([]byte, error) {
	var data []byte
	err := db.read.QueryRowContext(ctx, `SELECT data FROM ai_settings WHERE id=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

func (db *DB) SaveAISettings(ctx context.Context, data []byte, now int64) error {
	if len(data) == 0 {
		return errors.New("encrypted AI settings required")
	}
	if len(data) > maxAISettingsBytes {
		return errors.New("AI settings too large")
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	_, err := db.write.ExecContext(ctx, `INSERT INTO ai_settings(id,data,updated_at) VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, data, now)
	return err
}

func (db *DB) DeleteAISettings(ctx context.Context) error {
	_, err := db.write.ExecContext(ctx, `DELETE FROM ai_settings WHERE id=1`)
	return err
}

func (db *DB) GetAICache(ctx context.Context, targetID, kind, targetLang string) (*AICacheEntry, error) {
	if targetID == "" || kind == "" || targetLang == "" {
		return nil, nil
	}
	var e AICacheEntry
	err := db.read.QueryRowContext(ctx, `SELECT target_id,account_id,kind,target_lang,content_hash,model,payload,created_at,updated_at
		FROM message_ai_cache WHERE target_id=? AND kind=? AND target_lang=?`,
		targetID, kind, targetLang).Scan(
		&e.TargetID, &e.AccountID, &e.Kind, &e.TargetLang, &e.ContentHash, &e.Model, &e.Payload, &e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (db *DB) SaveAICache(ctx context.Context, entry AICacheEntry) error {
	if entry.TargetID == "" || entry.Kind == "" || entry.TargetLang == "" {
		return errors.New("invalid AI cache key")
	}
	if len(entry.Payload) > maxAICacheBytes {
		return errors.New("AI cache payload too large")
	}
	now := entry.UpdatedAt
	if now <= 0 {
		now = time.Now().Unix()
	}
	created := entry.CreatedAt
	if created <= 0 {
		created = now
	}
	_, err := db.write.ExecContext(ctx, `INSERT INTO message_ai_cache(target_id,account_id,kind,target_lang,content_hash,model,payload,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(target_id,kind,target_lang) DO UPDATE SET
			account_id=excluded.account_id,
			content_hash=excluded.content_hash,
			model=excluded.model,
			payload=excluded.payload,
			updated_at=excluded.updated_at`,
		entry.TargetID, entry.AccountID, entry.Kind, entry.TargetLang, entry.ContentHash, entry.Model, entry.Payload, created, now)
	return err
}

func (db *DB) ClearAICache(ctx context.Context) (int64, error) {
	res, err := db.write.ExecContext(ctx, `DELETE FROM message_ai_cache`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (db *DB) AICacheStats(ctx context.Context) (AICacheStats, error) {
	var stats AICacheStats
	err := db.read.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN kind IN ('summary','thread_summary') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN kind = 'translation' THEN 1 ELSE 0 END), 0)
		FROM message_ai_cache`).Scan(&stats.SummaryCount, &stats.TranslationCount)
	return stats, err
}
