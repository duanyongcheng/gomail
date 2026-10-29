-- Keep provider's native thread_id in messages; local grouping is account-scoped.
CREATE TABLE message_threads (
    message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('native','headers')),
    thread_key TEXT NOT NULL
);
CREATE INDEX message_threads_group ON message_threads(account_id,kind,thread_key);
