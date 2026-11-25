CREATE TABLE ai_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    data BLOB NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE message_ai_cache (
    target_id TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK(kind IN ('summary', 'thread_summary', 'translation')),
    target_lang TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    model TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (target_id, kind, target_lang)
);

CREATE INDEX idx_message_ai_cache_account ON message_ai_cache(account_id);
CREATE INDEX idx_message_ai_cache_updated ON message_ai_cache(updated_at);
