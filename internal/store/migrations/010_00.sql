CREATE TABLE IF NOT EXISTS secure_user_data (
key TEXT PRIMARY KEY,
value BLOB NOT NULL,
updated_at INTEGER NOT NULL
);
ALTER TABLE pending_mail_ops ADD COLUMN next_retry_at INTEGER;
CREATE INDEX IF NOT EXISTS idx_pending_mail_ops_retry
ON pending_mail_ops(status, next_retry_at, updated_at);
