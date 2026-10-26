CREATE TEMP TABLE v16_message_dedup (
loser_id TEXT PRIMARY KEY,
winner_id TEXT NOT NULL
);
INSERT INTO v16_message_dedup (loser_id, winner_id)
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

CREATE TEMP TABLE v16_imap_relation_dedup (
loser_id TEXT NOT NULL,
winner_id TEXT NOT NULL,
folder_id TEXT NOT NULL,
PRIMARY KEY (loser_id, folder_id)
);
INSERT INTO v16_imap_relation_dedup
(loser_id, winner_id, folder_id)
SELECT message_id, winner_id, folder_id
FROM (
SELECT
message.id AS message_id,
relation.folder_id AS folder_id,
FIRST_VALUE(message.id) OVER (
PARTITION BY message.account_id,
relation.folder_id,
message.remote_id
ORDER BY message.updated_at DESC,
message.created_at DESC,
message.id DESC
) AS winner_id
FROM messages AS message
JOIN accounts AS account ON account.id = message.account_id
JOIN message_folders AS relation
ON relation.message_id = message.id
WHERE message.is_deleted = 0
AND message.remote_id != ''
AND account.provider = 'imap'
)
WHERE message_id != winner_id;

DELETE FROM message_folders
WHERE EXISTS (
SELECT 1
FROM v16_imap_relation_dedup AS dedup
WHERE dedup.loser_id = message_folders.message_id
AND dedup.folder_id = message_folders.folder_id
);

INSERT OR IGNORE INTO v16_message_dedup (loser_id, winner_id)
SELECT relation.loser_id, MAX(relation.winner_id)
FROM v16_imap_relation_dedup AS relation
WHERE NOT EXISTS (
SELECT 1 FROM message_folders
WHERE message_id = relation.loser_id
)
GROUP BY relation.loser_id;
