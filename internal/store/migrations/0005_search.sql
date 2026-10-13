-- Talaria migration 0005: full-text search over messages (SQLite FTS5).
-- The index mirrors live, non-deleted, non-system messages and is kept in
-- sync by triggers.

CREATE VIRTUAL TABLE messages_fts USING fts5(
  content,
  message_id UNINDEXED,
  channel_id UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);

INSERT INTO messages_fts (content, message_id, channel_id)
  SELECT content, id, channel_id FROM messages
  WHERE kind = 'default' AND deleted_at IS NULL AND content <> '';

CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages
WHEN new.kind = 'default' AND new.content <> ''
BEGIN
  INSERT INTO messages_fts (content, message_id, channel_id) VALUES (new.content, new.id, new.channel_id);
END;

CREATE TRIGGER messages_fts_au AFTER UPDATE OF content, deleted_at ON messages
BEGIN
  DELETE FROM messages_fts WHERE message_id = old.id;
  INSERT INTO messages_fts (content, message_id, channel_id)
    SELECT new.content, new.id, new.channel_id
    WHERE new.deleted_at IS NULL AND new.kind = 'default' AND new.content <> '';
END;

CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages
BEGIN
  DELETE FROM messages_fts WHERE message_id = old.id;
END;
