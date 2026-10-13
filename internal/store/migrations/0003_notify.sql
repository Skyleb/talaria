-- Talaria migration 0003: notifications (Web Push), per-user channel mutes,
-- and @mentions on messages.

CREATE TABLE push_subscriptions (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint   TEXT NOT NULL UNIQUE,
  p256dh     TEXT NOT NULL,
  auth       TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX idx_push_user ON push_subscriptions(user_id);

CREATE TABLE channel_mutes (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, channel_id)
);

-- Comma-separated mentioned user ids; empty when none.
ALTER TABLE messages ADD COLUMN mention_ids TEXT NOT NULL DEFAULT '';
