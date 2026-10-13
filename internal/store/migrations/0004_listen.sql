-- Talaria migration 0004: synchronized listening is scoped to a channel (a
-- group channel, a DM, or a group chat), not just a group.

ALTER TABLE playback_sessions ADD COLUMN channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE;
CREATE INDEX idx_playback_sessions_channel ON playback_sessions(channel_id, ended_at);

ALTER TABLE playlists ADD COLUMN channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE;
CREATE INDEX idx_playlists_channel ON playlists(channel_id);
