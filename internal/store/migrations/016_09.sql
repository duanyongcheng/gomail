DELETE FROM messages
WHERE id IN (SELECT loser_id FROM v16_message_dedup);
DROP TABLE v16_imap_relation_dedup;
DROP TABLE v16_message_dedup;
