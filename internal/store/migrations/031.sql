CREATE TABLE gmail_draft_copies_next (
    draft_id TEXT PRIMARY KEY REFERENCES compose_drafts(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    remote_id TEXT NOT NULL DEFAULT '',
    remote_message_id TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('pending','synced','conflict','delete_pending','sending','sent')),
    data BLOB NOT NULL,
    outbox_id TEXT REFERENCES outbox(id)
);
INSERT INTO gmail_draft_copies_next
    (draft_id,account_id,remote_id,remote_message_id,revision,state,data)
    SELECT draft_id,account_id,remote_id,remote_message_id,revision,state,data
    FROM gmail_draft_copies;
DROP TABLE gmail_draft_copies;
ALTER TABLE gmail_draft_copies_next RENAME TO gmail_draft_copies;
CREATE UNIQUE INDEX gmail_draft_copy_remote
    ON gmail_draft_copies(account_id,remote_id) WHERE remote_id != '';
CREATE UNIQUE INDEX gmail_draft_copy_outbox
    ON gmail_draft_copies(outbox_id) WHERE outbox_id IS NOT NULL;
