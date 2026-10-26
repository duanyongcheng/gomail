INSERT INTO v14_message_dedup (loser_id, winner_id)
SELECT id, winner_id
FROM (
SELECT
id,
FIRST_VALUE(id) OVER (
PARTITION BY account_id, remote_id
ORDER BY updated_at DESC, created_at DESC, id DESC
) AS winner_id
FROM messages
WHERE is_deleted = 0
AND remote_id != ''
AND remote_id GLOB '*[^0-9]*'
)
WHERE id != winner_id;
