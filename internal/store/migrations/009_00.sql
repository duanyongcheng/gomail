CREATE TABLE IF NOT EXISTS pending_mail_ops (
id TEXT PRIMARY KEY,
account_id TEXT NOT NULL,
message_id TEXT NOT NULL,
op_type TEXT NOT NULL,
payload_json TEXT NOT NULL,
status TEXT NOT NULL CHECK(status IN ('pending', 'in_progress', 'failed', 'done')),
attempts INTEGER NOT NULL DEFAULT 0,
last_error TEXT,
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pending_mail_ops_account_status
ON pending_mail_ops(account_id, status, updated_at);
