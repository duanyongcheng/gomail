CREATE TRIGGER IF NOT EXISTS trg_messages_non_imap_remote_unique_insert
BEFORE INSERT ON messages
WHEN NEW.is_deleted = 0
AND NEW.remote_id != ''
AND COALESCE((SELECT provider FROM accounts WHERE id = NEW.account_id), '') != 'imap'
AND EXISTS (
SELECT 1 FROM messages existing
WHERE existing.account_id = NEW.account_id
AND existing.remote_id = NEW.remote_id
AND existing.is_deleted = 0
)
BEGIN
SELECT RAISE(ABORT, 'duplicate live non-IMAP remote_id');
END;

CREATE TRIGGER IF NOT EXISTS trg_messages_non_imap_remote_unique_update
BEFORE UPDATE OF account_id, remote_id, is_deleted ON messages
WHEN NEW.is_deleted = 0
AND NEW.remote_id != ''
AND COALESCE((SELECT provider FROM accounts WHERE id = NEW.account_id), '') != 'imap'
AND EXISTS (
SELECT 1 FROM messages existing
WHERE existing.id != OLD.id
AND existing.account_id = NEW.account_id
AND existing.remote_id = NEW.remote_id
AND existing.is_deleted = 0
)
BEGIN
SELECT RAISE(ABORT, 'duplicate live non-IMAP remote_id');
END;

CREATE TRIGGER IF NOT EXISTS trg_message_folders_imap_uid_unique_insert
BEFORE INSERT ON message_folders
WHEN COALESCE((
SELECT account.provider
FROM messages message
JOIN accounts account ON account.id = message.account_id
WHERE message.id = NEW.message_id
), '') = 'imap'
AND COALESCE((SELECT is_deleted FROM messages WHERE id = NEW.message_id), 1) = 0
AND COALESCE((SELECT remote_id FROM messages WHERE id = NEW.message_id), '') != ''
AND EXISTS (
SELECT 1
FROM message_folders relation
JOIN messages existing ON existing.id = relation.message_id
WHERE relation.folder_id = NEW.folder_id
AND existing.id != NEW.message_id
AND existing.is_deleted = 0
AND existing.account_id = (SELECT account_id FROM messages WHERE id = NEW.message_id)
AND existing.remote_id = (SELECT remote_id FROM messages WHERE id = NEW.message_id)
)
BEGIN
SELECT RAISE(ABORT, 'duplicate live IMAP UID in folder');
END;

CREATE TRIGGER IF NOT EXISTS trg_message_folders_imap_uid_unique_update
BEFORE UPDATE OF message_id, folder_id ON message_folders
WHEN COALESCE((
SELECT account.provider
FROM messages message
JOIN accounts account ON account.id = message.account_id
WHERE message.id = NEW.message_id
), '') = 'imap'
AND COALESCE((SELECT is_deleted FROM messages WHERE id = NEW.message_id), 1) = 0
AND COALESCE((SELECT remote_id FROM messages WHERE id = NEW.message_id), '') != ''
AND EXISTS (
SELECT 1
FROM message_folders relation
JOIN messages existing ON existing.id = relation.message_id
WHERE relation.folder_id = NEW.folder_id
AND existing.id != NEW.message_id
AND existing.is_deleted = 0
AND existing.account_id = (SELECT account_id FROM messages WHERE id = NEW.message_id)
AND existing.remote_id = (SELECT remote_id FROM messages WHERE id = NEW.message_id)
)
BEGIN
SELECT RAISE(ABORT, 'duplicate live IMAP UID in folder');
END;

CREATE TRIGGER IF NOT EXISTS trg_messages_imap_uid_unique_update
BEFORE UPDATE OF account_id, remote_id, is_deleted ON messages
WHEN NEW.is_deleted = 0
AND NEW.remote_id != ''
AND COALESCE((SELECT provider FROM accounts WHERE id = NEW.account_id), '') = 'imap'
AND EXISTS (
SELECT 1
FROM message_folders own_relation
JOIN message_folders other_relation
ON other_relation.folder_id = own_relation.folder_id
AND other_relation.message_id != OLD.id
JOIN messages existing ON existing.id = other_relation.message_id
WHERE own_relation.message_id = OLD.id
AND existing.is_deleted = 0
AND existing.account_id = NEW.account_id
AND existing.remote_id = NEW.remote_id
)
BEGIN
SELECT RAISE(ABORT, 'duplicate live IMAP UID in folder');
END;
