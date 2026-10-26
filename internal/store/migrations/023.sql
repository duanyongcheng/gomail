CREATE TABLE gmail_message_failures (
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    remote_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('too_large', 'invalid_content')),
    attempts INTEGER NOT NULL CHECK (attempts > 0),
    next_retry_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    notify_on_recovery INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, remote_id)
);
CREATE INDEX idx_gmail_message_failures_due ON gmail_message_failures(account_id, next_retry_at, remote_id);
