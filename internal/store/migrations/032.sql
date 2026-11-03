CREATE TABLE provider_checkpoints (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 provider TEXT NOT NULL,
 scope TEXT NOT NULL,
 data BLOB NOT NULL,
 PRIMARY KEY(account_id,provider,scope)
);
CREATE TABLE provider_mappings (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 provider TEXT NOT NULL,
 scope TEXT NOT NULL,
 remote_id TEXT NOT NULL,
 message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
 PRIMARY KEY(account_id,provider,scope,remote_id)
);
CREATE UNIQUE INDEX protocol_mailbox_identity ON accounts(provider,lower(trim(email))) WHERE provider IN ('imap','pop3','outlook');
CREATE TABLE network_settings (id INTEGER PRIMARY KEY CHECK(id=1),data BLOB NOT NULL);
