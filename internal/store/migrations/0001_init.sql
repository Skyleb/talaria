-- Lantern migration 0001: Phase 1 core schema.
-- IDs are ULIDs (TEXT, lexicographically time-ordered). Timestamps are Unix ms.

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE users (
  id             TEXT PRIMARY KEY,
  username       TEXT NOT NULL COLLATE NOCASE UNIQUE,
  display_name   TEXT NOT NULL,
  password_hash  TEXT NOT NULL,
  avatar_file_id TEXT REFERENCES files(id),
  status_text    TEXT NOT NULL DEFAULT '',
  is_admin       INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  disabled_at    INTEGER
);

CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  user_agent   TEXT NOT NULL DEFAULT '',
  ip           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE invites (
  code       TEXT PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('registration','group')),
  group_id   TEXT REFERENCES groups(id) ON DELETE CASCADE,
  created_by TEXT NOT NULL REFERENCES users(id),
  max_uses   INTEGER,
  use_count  INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER,
  revoked_at INTEGER,
  created_at INTEGER NOT NULL
);

CREATE TABLE friendships (
  id           TEXT PRIMARY KEY,
  requester_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  addressee_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status       TEXT NOT NULL CHECK (status IN ('pending','accepted','declined','blocked')),
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  pair_key     TEXT NOT NULL UNIQUE
);
CREATE INDEX idx_friend_addr ON friendships(addressee_id, status);

CREATE TABLE groups (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  icon_file_id TEXT REFERENCES files(id),
  owner_id     TEXT NOT NULL REFERENCES users(id),
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

CREATE TABLE group_members (
  group_id  TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role      TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
  joined_at INTEGER NOT NULL,
  PRIMARY KEY (group_id, user_id)
);
CREATE INDEX idx_gm_user ON group_members(user_id);

-- channels.kind has NO CHECK constraint: Phase 2 adds 'group_voice' additively
-- (SQLite cannot alter CHECK constraints without a table rebuild).
CREATE TABLE channels (
  id         TEXT PRIMARY KEY,
  kind       TEXT NOT NULL,
  group_id   TEXT REFERENCES groups(id) ON DELETE CASCADE,
  name       TEXT,
  topic      TEXT NOT NULL DEFAULT '',
  position   INTEGER NOT NULL DEFAULT 0,
  dm_key     TEXT UNIQUE,
  created_at INTEGER NOT NULL
);
CREATE INDEX idx_channels_group ON channels(group_id);

CREATE TABLE dm_participants (
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX idx_dmp_user ON dm_participants(user_id);

CREATE TABLE messages (
  id          TEXT PRIMARY KEY,
  channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  author_id   TEXT NOT NULL REFERENCES users(id),
  kind        TEXT NOT NULL DEFAULT 'default',
  content     TEXT NOT NULL DEFAULT '',
  reply_to_id TEXT REFERENCES messages(id),
  created_at  INTEGER NOT NULL,
  edited_at   INTEGER,
  deleted_at  INTEGER
);
CREATE INDEX idx_messages_channel ON messages(channel_id, id);

CREATE TABLE files (
  id         TEXT PRIMARY KEY,
  sha256     TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  mime       TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX idx_files_sha ON files(sha256);

CREATE TABLE attachments (
  id          TEXT PRIMARY KEY,
  file_id     TEXT NOT NULL REFERENCES files(id),
  message_id  TEXT REFERENCES messages(id) ON DELETE CASCADE,
  uploader_id TEXT NOT NULL REFERENCES users(id),
  file_name   TEXT NOT NULL,
  width       INTEGER,
  height      INTEGER,
  duration_ms INTEGER,
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_attach_msg ON attachments(message_id);

CREATE TABLE read_states (
  user_id              TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_id           TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  last_read_message_id TEXT,
  updated_at           INTEGER NOT NULL,
  PRIMARY KEY (user_id, channel_id)
);
