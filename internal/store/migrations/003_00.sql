CREATE INDEX IF NOT EXISTS idx_messages_account_remote ON messages(account_id, remote_id);
CREATE INDEX IF NOT EXISTS idx_snoozed_unsnoozed_at ON snoozed_messages(unsnoozed_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_folders_account_remote ON folders(account_id, remote_id);
