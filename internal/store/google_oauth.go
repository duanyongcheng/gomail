package store

import (
	"context"
	"database/sql"
	"errors"
)

func (db *DB) GoogleOAuthSettings(ctx context.Context) ([]byte, error) {
	var data []byte
	err := db.read.QueryRowContext(ctx, `SELECT data FROM google_oauth_settings WHERE id=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

func (db *DB) SaveGoogleOAuthSettings(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return errors.New("encrypted Google OAuth settings required")
	}
	if len(data) > 16<<10 {
		return errors.New("Google OAuth settings too large")
	}
	_, err := db.write.ExecContext(ctx, `INSERT INTO google_oauth_settings(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, data)
	return err
}

func (db *DB) DeleteGoogleOAuthSettings(ctx context.Context) error {
	_, err := db.write.ExecContext(ctx, `DELETE FROM google_oauth_settings WHERE id=1`)
	return err
}
