CREATE TABLE IF NOT EXISTS ai_config (
id TEXT PRIMARY KEY DEFAULT 'active',
provider_type TEXT NOT NULL CHECK(provider_type IN ('openai_compatible', 'generic')),
config TEXT NOT NULL DEFAULT '{}',
is_enabled INTEGER NOT NULL DEFAULT 1,
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL
);
