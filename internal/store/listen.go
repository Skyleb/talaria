package store

import (
	"context"
	"database/sql"

	"github.com/skyle/talaria/internal/model"
)

// --- Playlists ---

const playlistCols = `id, owner_id, channel_id, name, archived_at, created_at, updated_at`

func scanPlaylist(row interface{ Scan(...any) error }) (*model.Playlist, error) {
	var p model.Playlist
	if err := row.Scan(&p.ID, &p.OwnerID, nstr{&p.ChannelID}, &p.Name, nint{&p.ArchivedAt}, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePlaylist makes an empty playlist. An empty name marks a live queue,
// which is hidden from the saved-playlists list.
func (s *Store) CreatePlaylist(ctx context.Context, ownerID, channelID, name string) (*model.Playlist, error) {
	p := &model.Playlist{ID: NewID(), OwnerID: ownerID, ChannelID: channelID, Name: name, CreatedAt: now()}
	p.UpdatedAt = p.CreatedAt
	_, err := s.db.ExecContext(ctx, `INSERT INTO playlists (id, owner_id, channel_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, p.ID, p.OwnerID, p.ChannelID, p.Name, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) PlaylistByID(ctx context.Context, id string) (*model.Playlist, error) {
	return scanPlaylist(s.db.QueryRowContext(ctx, `SELECT `+playlistCols+` FROM playlists WHERE id = ?`, id))
}

// SavedPlaylistsByChannel lists named (non-queue) playlists for a channel
// with their item counts, most recently updated first.
func (s *Store) SavedPlaylistsByChannel(ctx context.Context, channelID string) ([]*model.Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.owner_id, p.channel_id, p.name, p.archived_at, p.created_at, p.updated_at,
		(SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id = p.id)
		FROM playlists p WHERE p.channel_id = ? AND p.name <> '' ORDER BY p.updated_at DESC`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Playlist{}
	for rows.Next() {
		var p model.Playlist
		if err := rows.Scan(&p.ID, &p.OwnerID, nstr{&p.ChannelID}, &p.Name, nint{&p.ArchivedAt}, &p.CreatedAt, &p.UpdatedAt, &p.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ClonePlaylist copies the source playlist's items into a new playlist. Used
// both to start a session from a saved playlist and to save a live queue.
func (s *Store) ClonePlaylist(ctx context.Context, srcID, ownerID, channelID, name string) (*model.Playlist, error) {
	items, err := s.ListPlaylistItems(ctx, srcID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p := &model.Playlist{ID: NewID(), OwnerID: ownerID, ChannelID: channelID, Name: name, CreatedAt: now()}
	p.UpdatedAt = p.CreatedAt
	if _, err := tx.ExecContext(ctx, `INSERT INTO playlists (id, owner_id, channel_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, p.ID, p.OwnerID, p.ChannelID, p.Name, p.CreatedAt, p.UpdatedAt); err != nil {
		return nil, err
	}
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items
			(id, playlist_id, position, kind, title, artist, duration_ms, refs_json, upload_attachment_id, added_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			NewID(), p.ID, it.Position, it.Kind, it.Title, nullStr(it.Artist), nullInt(it.DurationMS),
			it.RefsJSON, nullStr(it.AttachmentID), it.AddedBy, now()); err != nil {
			return nil, err
		}
	}
	return p, tx.Commit()
}

func (s *Store) DeletePlaylist(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM playlists WHERE id = ?`, id)
	return err
}

func (s *Store) TouchPlaylist(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playlists SET updated_at = ? WHERE id = ?`, now(), id)
	return err
}

// --- Playlist items ---

const itemCols = `id, playlist_id, position, kind, title, artist, duration_ms, refs_json, upload_attachment_id, added_by, created_at`

func scanItem(row interface{ Scan(...any) error }) (*model.PlaylistItem, error) {
	var it model.PlaylistItem
	if err := row.Scan(&it.ID, &it.PlaylistID, &it.Position, &it.Kind, &it.Title, nstr{&it.Artist},
		nint{&it.DurationMS}, &it.RefsJSON, nstr{&it.AttachmentID}, &it.AddedBy, &it.CreatedAt); err != nil {
		return nil, err
	}
	return &it, nil
}

// AppendPlaylistItem adds an item at the end of the playlist.
func (s *Store) AppendPlaylistItem(ctx context.Context, it *model.PlaylistItem) error {
	if it.ID == "" {
		it.ID = NewID()
	}
	it.CreatedAt = now()
	var maxPos sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(position) FROM playlist_items WHERE playlist_id = ?`, it.PlaylistID).Scan(&maxPos); err != nil {
		return err
	}
	it.Position = maxPos.Float64 + 1
	if it.RefsJSON == "" {
		it.RefsJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO playlist_items
		(id, playlist_id, position, kind, title, artist, duration_ms, refs_json, upload_attachment_id, added_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.PlaylistID, it.Position, it.Kind, it.Title, nullStr(it.Artist), nullInt(it.DurationMS),
		it.RefsJSON, nullStr(it.AttachmentID), it.AddedBy, it.CreatedAt)
	if err != nil {
		return err
	}
	return s.TouchPlaylist(ctx, it.PlaylistID)
}

func (s *Store) PlaylistItemByID(ctx context.Context, id string) (*model.PlaylistItem, error) {
	return scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM playlist_items WHERE id = ?`, id))
}

func (s *Store) ListPlaylistItems(ctx context.Context, playlistID string) ([]*model.PlaylistItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM playlist_items
		WHERE playlist_id = ? ORDER BY position`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.PlaylistItem{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) DeletePlaylistItem(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM playlist_items WHERE id = ?`, id)
	return err
}

// SetPlaylistItemDuration records a duration learned client-side on first
// play (YouTube's oEmbed does not expose it); never overwrites a known value.
func (s *Store) SetPlaylistItemDuration(ctx context.Context, id string, durationMS int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playlist_items SET duration_ms = ?
		WHERE id = ? AND (duration_ms IS NULL OR duration_ms = 0)`, durationMS, id)
	return err
}

// PlaylistChannelForAttachment finds the channel of a playlist referencing the
// attachment, so channel members may stream queue uploads that have no message.
func (s *Store) PlaylistChannelForAttachment(ctx context.Context, attachmentID string) (string, bool) {
	var ch sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT p.channel_id FROM playlist_items i
		JOIN playlists p ON p.id = i.playlist_id
		WHERE i.upload_attachment_id = ? LIMIT 1`, attachmentID).Scan(&ch)
	return ch.String, err == nil && ch.Valid
}

// --- Playback sessions ---

const sessionCols = `id, channel_id, host_user_id, playlist_id, current_item_id, state, position_ms, anchor_server_ms, started_at, ended_at`

func scanSession(row interface{ Scan(...any) error }) (*model.PlaybackSession, error) {
	var ps model.PlaybackSession
	if err := row.Scan(&ps.ID, nstr{&ps.ChannelID}, &ps.HostUserID, &ps.PlaylistID, nstr{&ps.CurrentItemID},
		&ps.State, &ps.PositionMS, &ps.AnchorServerMS, &ps.StartedAt, nint{&ps.EndedAt}); err != nil {
		return nil, err
	}
	return &ps, nil
}

func (s *Store) CreatePlaybackSession(ctx context.Context, channelID, hostID, playlistID, firstItemID string) (*model.PlaybackSession, error) {
	ps := &model.PlaybackSession{
		ID: NewID(), ChannelID: channelID, HostUserID: hostID, PlaylistID: playlistID,
		CurrentItemID: firstItemID, State: "paused", AnchorServerMS: now(), StartedAt: now(),
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO playback_sessions
		(id, channel_id, host_user_id, playlist_id, current_item_id, state, position_ms, anchor_server_ms, started_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		ps.ID, ps.ChannelID, ps.HostUserID, ps.PlaylistID, nullStr(ps.CurrentItemID), ps.State, ps.AnchorServerMS, ps.StartedAt)
	if err != nil {
		return nil, err
	}
	return ps, nil
}

func (s *Store) PlaybackSessionByID(ctx context.Context, id string) (*model.PlaybackSession, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM playback_sessions WHERE id = ?`, id))
}

// ActivePlaybackSession returns the channel's live session, or ErrNotFound.
func (s *Store) ActivePlaybackSession(ctx context.Context, channelID string) (*model.PlaybackSession, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM playback_sessions
		WHERE channel_id = ? AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`, channelID))
}

// ActivePlaybackSessions lists every live session.
func (s *Store) ActivePlaybackSessions(ctx context.Context) ([]*model.PlaybackSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM playback_sessions WHERE ended_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.PlaybackSession
	for rows.Next() {
		ps, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePlayback(ctx context.Context, id, state string, positionMS, anchorMS int64, currentItemID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playback_sessions
		SET state = ?, position_ms = ?, anchor_server_ms = ?, current_item_id = ? WHERE id = ?`,
		state, positionMS, anchorMS, nullStr(currentItemID), id)
	return err
}

func (s *Store) SetPlaybackHost(ctx context.Context, id, hostID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playback_sessions SET host_user_id = ? WHERE id = ?`, hostID, id)
	return err
}

func (s *Store) EndPlaybackSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playback_sessions SET state = 'ended', ended_at = ?
		WHERE id = ? AND ended_at IS NULL`, now(), id)
	return err
}

// EndDanglingPlaybackSessions closes sessions left open by an unclean shutdown.
func (s *Store) EndDanglingPlaybackSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playback_sessions SET state = 'ended', ended_at = ? WHERE ended_at IS NULL`, now())
	return err
}

// --- Session members ---

func (s *Store) JoinPlayback(ctx context.Context, sessionID, userID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO playback_members (session_id, user_id, joined_at)
		VALUES (?, ?, ?) ON CONFLICT(session_id, user_id) DO UPDATE SET left_at = NULL, joined_at = excluded.joined_at`,
		sessionID, userID, now())
	return err
}

func (s *Store) LeavePlayback(ctx context.Context, sessionID, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE playback_members SET left_at = ?
		WHERE session_id = ? AND user_id = ? AND left_at IS NULL`, now(), sessionID, userID)
	return err
}

// PlaybackMemberIDs lists current listeners, longest-tenured first (the
// natural order for host succession).
func (s *Store) PlaybackMemberIDs(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM playback_members
		WHERE session_id = ? AND left_at IS NULL ORDER BY joined_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SessionsForUser lists live sessions the user is currently listening in.
func (s *Store) SessionsForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.session_id FROM playback_members m
		JOIN playback_sessions ps ON ps.id = m.session_id
		WHERE m.user_id = ? AND m.left_at IS NULL AND ps.ended_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
