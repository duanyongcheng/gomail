-- Non-idempotent sends are deliberately separate from cached-message mutations.
CREATE TABLE outbox (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    request_key TEXT NOT NULL,
    data BLOB,
    summary BLOB,
    status TEXT NOT NULL CHECK(status IN ('queued','preparing','in_progress','sent','failed','outcome_unknown','cancelled','dismissed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    retryable INTEGER NOT NULL DEFAULT 0,
    next_retry_at INTEGER,
    last_error TEXT NOT NULL DEFAULT '',
    remote_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(account_id,request_key)
);
CREATE INDEX outbox_due ON outbox(status,retryable,next_retry_at,created_at);
