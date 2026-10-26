CREATE TABLE contacts (
id TEXT PRIMARY KEY,
display_name TEXT NOT NULL DEFAULT '',
notes TEXT NOT NULL DEFAULT '',
is_favorite INTEGER NOT NULL DEFAULT 0 CHECK(is_favorite IN (0, 1)),
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL
);
CREATE TABLE contact_emails (
id TEXT PRIMARY KEY,
contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
address TEXT NOT NULL,
normalized_address TEXT NOT NULL COLLATE NOCASE,
label TEXT NOT NULL DEFAULT 'other'
CHECK(label IN ('work', 'personal', 'other')),
is_primary INTEGER NOT NULL DEFAULT 0 CHECK(is_primary IN (0, 1)),
created_at INTEGER NOT NULL,
UNIQUE(normalized_address)
);
CREATE INDEX idx_contact_emails_contact
ON contact_emails(contact_id);
CREATE UNIQUE INDEX idx_contact_emails_one_primary
ON contact_emails(contact_id) WHERE is_primary = 1;
CREATE TABLE contact_suggestion_suppressions (
normalized_address TEXT PRIMARY KEY COLLATE NOCASE,
created_at INTEGER NOT NULL
);
