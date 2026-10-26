INSERT INTO kanban_cards
(message_id, column_name, position, created_at, updated_at)
SELECT dedup.winner_id, relation.column_name, relation.position,
relation.created_at, relation.updated_at
FROM kanban_cards AS relation
JOIN v16_message_dedup AS dedup
ON dedup.loser_id = relation.message_id
WHERE 1
ON CONFLICT(message_id) DO UPDATE SET
column_name = excluded.column_name,
position = excluded.position,
created_at = excluded.created_at,
updated_at = excluded.updated_at
WHERE excluded.updated_at > kanban_cards.updated_at;
