INSERT INTO v14_message_dedup (loser_id, winner_id)
SELECT id, winner_id
FROM (
SELECT
message.id AS id,
FIRST_VALUE(message.id) OVER (
PARTITION BY message.account_id, message.remote_id
ORDER BY message.updated_at DESC,
message.created_at DESC,
message.id DESC
) AS winner_id
FROM messages AS message
JOIN accounts AS account ON account.id = message.account_id
WHERE message.is_deleted = 0
AND message.remote_id != ''
AND account.provider != 'imap'
)
WHERE id != winner_id;
