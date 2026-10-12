-- Lantern migration 0002: forward-looking tables for Phases 2-4 so later
-- changes stay additive. Unused tables cost nothing.

-- Phase 2: calls
CREATE TABLE call_sessions (
  id         TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('p2p','sfu')),
  started_by TEXT NOT NULL REFERENCES users(id),
  started_at INTEGER NOT NULL,
  ended_at   INTEGER
);

CREATE TABLE call_participants (
  call_id   TEXT NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
  user_id   TEXT NOT NULL REFERENCES users(id),
  joined_at INTEGER NOT NULL,
  left_at   INTEGER,
  PRIMARY KEY (call_id, user_id, joined_at)
);

-- Phase 3: presence (live presence is in-memory; this persists last-known state
-- and auth tokens for the optional native helper).
CREATE TABLE presence_snapshots (
  user_id       TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  status        TEXT NOT NULL DEFAULT 'offline',
  activity_json TEXT,
  updated_at    INTEGER NOT NULL
);

CREATE TABLE helper_tokens (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label      TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  revoked_at INTEGER
);

-- Phase 4: music/video providers, playlists, synchronized playback
CREATE TABLE provider_links (
  id                TEXT PRIMARY KEY,
  user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider          TEXT NOT NULL CHECK (provider IN ('spotify','apple_music','youtube','upload')),
  provider_user_id  TEXT,
  access_token_enc  TEXT,
  refresh_token_enc TEXT,
  scopes            TEXT,
  expires_at        INTEGER,
  created_at        INTEGER NOT NULL,
  UNIQUE (user_id, provider)
);

CREATE TABLE playlists (
  id          TEXT PRIMARY KEY,
  owner_id    TEXT NOT NULL REFERENCES users(id),
  group_id    TEXT REFERENCES groups(id) ON DELETE SET NULL,
  name        TEXT NOT NULL,
  archived_at INTEGER,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE playlist_items (
  id          TEXT PRIMARY KEY,
  playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  position    REAL NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('track','video','upload')),
  title       TEXT NOT NULL,
  artist      TEXT,
  duration_ms INTEGER,
  isrc        TEXT,
  refs_json   TEXT NOT NULL DEFAULT '{}',
  upload_attachment_id TEXT REFERENCES attachments(id),
  added_by    TEXT NOT NULL REFERENCES users(id),
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_pli_playlist ON playlist_items(playlist_id, position);

-- Server-authoritative playback clock: true position at any moment is
-- position_ms + (server_now - anchor_server_ms) while state='playing'.
CREATE TABLE playback_sessions (
  id               TEXT PRIMARY KEY,
  group_id         TEXT REFERENCES groups(id),
  host_user_id     TEXT NOT NULL REFERENCES users(id),
  playlist_id      TEXT NOT NULL REFERENCES playlists(id),
  current_item_id  TEXT REFERENCES playlist_items(id),
  state            TEXT NOT NULL CHECK (state IN ('playing','paused','ended')),
  position_ms      INTEGER NOT NULL DEFAULT 0,
  anchor_server_ms INTEGER NOT NULL,
  started_at       INTEGER NOT NULL,
  ended_at         INTEGER
);

CREATE TABLE playback_members (
  session_id    TEXT NOT NULL REFERENCES playback_sessions(id) ON DELETE CASCADE,
  user_id       TEXT NOT NULL REFERENCES users(id),
  provider_pref TEXT,
  joined_at     INTEGER NOT NULL,
  left_at       INTEGER,
  PRIMARY KEY (session_id, user_id)
);
