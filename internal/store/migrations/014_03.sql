INSERT OR IGNORE INTO message_folders (message_id, folder_id)
SELECT dedup.winner_id, relation.folder_id
FROM message_folders AS relation
JOIN v14_message_dedup AS dedup ON dedup.loser_id = relation.message_id;
