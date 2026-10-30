CREATE TABLE gmail_draft_copies (
    draft_id TEXT PRIMARY KEY REFERENCES compose_drafts(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    remote_id TEXT NOT NULL DEFAULT '',
    remote_message_id TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('pending','synced','conflict')),
    data BLOB NOT NULL
);
CREATE UNIQUE INDEX gmail_draft_copy_remote ON gmail_draft_copies(account_id,remote_id) WHERE remote_id != '';
