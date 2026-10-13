package store

import (
	"context"
	"strings"

	"github.com/skyle/talaria/internal/model"
)

const channelCols = `id, kind, group_id, name, topic, position, created_at`

func scanChannel(row interface{ Scan(...any) error }) (*model.Channel, error) {
	var c model.Channel
	err := row.Scan(&c.ID, &c.Kind, nstr{&c.GroupID}, nstr{&c.Name}, &c.Topic, &c.Position, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ChannelByID(ctx context.Context, id string) (*model.Channel, error) {
	return scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelCols+` FROM channels WHERE id = ?`, id))
}

func (s *Store) CreateGroupChannel(ctx context.Context, groupID, name, topic string, position int) (*model.Channel, error) {
	c := &model.Channel{
		ID: NewID(), Kind: model.ChannelGroupText, GroupID: groupID,
		Name: strings.TrimSpace(name), Topic: topic, Position: position, CreatedAt: now(),
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO channels (id, kind, group_id, name, topic, position, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, c.ID, c.Kind, c.GroupID, c.Name, c.Topic, c.Position, c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) UpdateChannel(ctx context.Context, id string, name, topic *string) error {
	sets := []string{}
	args := []any{}
	if name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*name))
	}
	if topic != nil {
		sets = append(sets, "topic = ?")
		args = append(args, *topic)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	return err
}

func (s *Store) ListChannelsByGroup(ctx context.Context, groupID string) ([]*model.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelCols+` FROM channels
		WHERE group_id = ? ORDER BY position, created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetOrCreateDM returns the DM channel for the pair, creating it if needed.
func (s *Store) GetOrCreateDM(ctx context.Context, a, b string) (*model.Channel, bool, error) {
	key := pairKey(a, b)
	c, err := scanChannel(s.db.QueryRowContext(ctx,
		`SELECT `+channelCols+` FROM channels WHERE dm_key = ?`, key))
	if err == nil {
		return c, false, nil
	}
	if err != ErrNotFound {
		return nil, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	c = &model.Channel{ID: NewID(), Kind: model.ChannelDM, CreatedAt: now()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO channels (id, kind, topic, position, dm_key, created_at)
		VALUES (?, ?, '', 0, ?, ?)`, c.ID, c.Kind, key, c.CreatedAt); err != nil {
		return nil, false, err
	}
	for _, uid := range []string{a, b} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO dm_participants (channel_id, user_id)
			VALUES (?, ?)`, c.ID, uid); err != nil {
			return nil, false, err
		}
	}
	return c, true, tx.Commit()
}

// CreateGroupDM creates a multi-person chat. It carries no dm_key, so it
// never collides with (or hijacks) the private 1:1 channel of any pair.
func (s *Store) CreateGroupDM(ctx context.Context, userIDs []string) (*model.Channel, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c := &model.Channel{ID: NewID(), Kind: model.ChannelDM, CreatedAt: now()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO channels (id, kind, topic, position, created_at)
		VALUES (?, ?, '', 0, ?)`, c.ID, c.Kind, c.CreatedAt); err != nil {
		return nil, err
	}
	for _, uid := range userIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO dm_participants (channel_id, user_id)
			VALUES (?, ?)`, c.ID, uid); err != nil {
			return nil, err
		}
	}
	return c, tx.Commit()
}

// IsPairDM reports whether the channel is a 1:1 chat (created with a dm_key),
// as opposed to a multi-person chat.
func (s *Store) IsPairDM(ctx context.Context, channelID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels
		WHERE id = ? AND dm_key IS NOT NULL`, channelID).Scan(&n)
	return n > 0, err
}

func (s *Store) AddDMParticipant(ctx context.Context, channelID, userID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO dm_participants (channel_id, user_id)
		VALUES (?, ?)`, channelID, userID)
	return err
}

func (s *Store) RemoveDMParticipant(ctx context.Context, channelID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dm_participants
		WHERE channel_id = ? AND user_id = ?`, channelID, userID)
	return err
}

func (s *Store) ListDMChannels(ctx context.Context, userID string) ([]*model.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.kind, c.group_id, c.name, c.topic, c.position, c.created_at
		FROM channels c JOIN dm_participants p ON p.channel_id = c.id
		WHERE p.user_id = ? ORDER BY c.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DMParticipantIDs(ctx context.Context, channelID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id FROM dm_participants WHERE channel_id = ?`, channelID)
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

// ChannelMemberIDs resolves the audience of a channel: group members for group
// channels, participants for DMs. This is the fan-out primitive the gateway uses.
func (s *Store) ChannelMemberIDs(ctx context.Context, ch *model.Channel) ([]string, error) {
	if ch.Kind == model.ChannelDM {
		return s.DMParticipantIDs(ctx, ch.ID)
	}
	return s.GroupMemberIDs(ctx, ch.GroupID)
}

// IsChannelMember reports whether userID may read/write the channel.
func (s *Store) IsChannelMember(ctx context.Context, ch *model.Channel, userID string) (bool, error) {
	if ch.Kind == model.ChannelDM {
		var n int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dm_participants
			WHERE channel_id = ? AND user_id = ?`, ch.ID, userID).Scan(&n)
		return n > 0, err
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_members
		WHERE group_id = ? AND user_id = ?`, ch.GroupID, userID).Scan(&n)
	return n > 0, err
}
