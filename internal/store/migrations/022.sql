CREATE TABLE gmail_backfill_pending (
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    remote_id TEXT NOT NULL,
    PRIMARY KEY (account_id, remote_id)
);
