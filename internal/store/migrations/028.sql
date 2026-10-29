-- One unresolved management write per Gmail account. No automatic replay:
-- a create without a receipt must be reconciled, never blindly repeated.
CREATE TABLE gmail_label_changes (
    account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('create','rename','delete')),
    remote_id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
