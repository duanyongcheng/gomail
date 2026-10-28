-- Local compose data is never placed in the remotely synchronized messages
-- table or plaintext FTS. Only routing IDs, sizes and timestamps are public.
CREATE TABLE compose_drafts (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    request_key TEXT NOT NULL,
    data BLOB,
    revision INTEGER NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1)),
    updated_at INTEGER NOT NULL,
    UNIQUE(account_id,request_key)
);
CREATE INDEX compose_drafts_account ON compose_drafts(account_id,deleted,updated_at);
CREATE TABLE compose_attachments (
    id TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    size INTEGER NOT NULL,
    released INTEGER NOT NULL DEFAULT 0 CHECK(released IN (0,1)),
    created_at INTEGER NOT NULL
);
CREATE TABLE compose_draft_attachments (
    draft_id TEXT NOT NULL REFERENCES compose_drafts(id) ON DELETE CASCADE,
    attachment_id TEXT NOT NULL REFERENCES compose_attachments(id),
    PRIMARY KEY(draft_id,attachment_id)
);
