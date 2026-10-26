INSERT OR IGNORE INTO message_labels (message_id, label_id)
SELECT dedup.winner_id, relation.label_id
FROM message_labels AS relation
JOIN v16_message_dedup AS dedup
ON dedup.loser_id = relation.message_id;
