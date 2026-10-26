CREATE TABLE IF NOT EXISTS search_pending (
message_id TEXT PRIMARY KEY,
operation TEXT NOT NULL CHECK(operation IN ('index', 'remove')),
created_at INTEGER NOT NULL
);
