UPDATE pending_mail_ops
SET message_id = (
SELECT winner_id FROM v16_message_dedup
WHERE loser_id = pending_mail_ops.message_id
)
WHERE message_id IN (SELECT loser_id FROM v16_message_dedup);
