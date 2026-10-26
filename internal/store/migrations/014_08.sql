INSERT INTO search_pending (message_id, operation, created_at)
SELECT dedup.winner_id, 'index', MAX(relation.created_at)
FROM search_pending AS relation
JOIN v14_message_dedup AS dedup ON dedup.loser_id = relation.message_id
GROUP BY dedup.winner_id
ON CONFLICT(message_id) DO UPDATE SET
operation = 'index',
created_at = MAX(search_pending.created_at, excluded.created_at);
UPDATE search_pending
SET operation = 'index'
WHERE message_id IN (SELECT winner_id FROM v14_message_dedup);
DELETE FROM search_pending
WHERE message_id IN (SELECT loser_id FROM v14_message_dedup);
