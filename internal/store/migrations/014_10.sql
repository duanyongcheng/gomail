DELETE FROM messages WHERE id IN (SELECT loser_id FROM v14_message_dedup);
DROP TABLE v14_message_dedup;
DROP INDEX IF EXISTS idx_messages_account_remote;
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_account_remote_unique
ON messages(account_id, remote_id)
WHERE is_deleted = 0
AND remote_id != ''
AND remote_id GLOB '*[^0-9]*';
