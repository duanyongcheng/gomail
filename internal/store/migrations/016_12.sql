CREATE INDEX IF NOT EXISTS idx_messages_account_remote
ON messages(account_id, remote_id);
