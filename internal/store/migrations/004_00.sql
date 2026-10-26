CREATE INDEX IF NOT EXISTS idx_message_folders_folder_id ON message_folders(folder_id);
CREATE INDEX IF NOT EXISTS idx_messages_account_starred ON messages(account_id, is_starred) WHERE is_starred = 1 AND is_deleted = 0;
CREATE INDEX IF NOT EXISTS idx_messages_thread_date ON messages(thread_id, date) WHERE thread_id IS NOT NULL AND is_deleted = 0;
