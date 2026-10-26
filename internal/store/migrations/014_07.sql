INSERT INTO snoozed_messages
(message_id, snoozed_at, unsnoozed_at, return_to)
SELECT dedup.winner_id, relation.snoozed_at, relation.unsnoozed_at,
relation.return_to
FROM snoozed_messages AS relation
JOIN v14_message_dedup AS dedup ON dedup.loser_id = relation.message_id
WHERE 1
ON CONFLICT(message_id) DO UPDATE SET
snoozed_at = excluded.snoozed_at,
unsnoozed_at = excluded.unsnoozed_at,
return_to = excluded.return_to
WHERE excluded.snoozed_at > snoozed_messages.snoozed_at;
