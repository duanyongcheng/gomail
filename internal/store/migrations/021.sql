-- GoMail search cache. v1-v20 preserve the baseline schema contract.
-- Cache and message changes are committed in the same transaction, including
-- deletes and soft deletes; startup never needs a provider-specific FTS table.
CREATE VIRTUAL TABLE message_search USING fts5(
    message_id UNINDEXED,
    account_id UNINDEXED,
    subject,
    body,
    from_address,
    tokenize='trigram'
);
INSERT INTO message_search(message_id,account_id,subject,body,from_address)
    SELECT id,account_id,subject,body_text,from_address FROM messages
    WHERE is_deleted=0;
CREATE TRIGGER trg_message_search_insert AFTER INSERT ON messages
WHEN NEW.is_deleted=0
BEGIN
    INSERT INTO message_search(message_id,account_id,subject,body,from_address)
    VALUES(NEW.id,NEW.account_id,NEW.subject,NEW.body_text,NEW.from_address);
END;
CREATE TRIGGER trg_message_search_update
AFTER UPDATE OF id,account_id,subject,body_text,from_address,is_deleted ON messages
BEGIN
    DELETE FROM message_search WHERE message_id=OLD.id;
    INSERT INTO message_search(message_id,account_id,subject,body,from_address)
    SELECT NEW.id,NEW.account_id,NEW.subject,NEW.body_text,NEW.from_address
    WHERE NEW.is_deleted=0;
END;
CREATE TRIGGER trg_message_search_delete AFTER DELETE ON messages
BEGIN
    DELETE FROM message_search WHERE message_id=OLD.id;
END;
