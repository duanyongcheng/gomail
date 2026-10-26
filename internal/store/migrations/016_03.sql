UPDATE attachments
SET message_id = (
SELECT winner_id FROM v16_message_dedup
WHERE loser_id = attachments.message_id
)
WHERE message_id IN (SELECT loser_id FROM v16_message_dedup);
