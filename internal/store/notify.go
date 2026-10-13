package store

import (
	"context"

	"github.com/skyleb/talaria/internal/model"
)

// --- Web Push subscriptions ---

func (s *Store) UpsertPushSubscription(ctx context.Context, sub *model.PushSubscription) error {
	if sub.ID == "" {
		sub.ID = NewID()
	}
	sub.CreatedAt = now()
	// A browser re-subscribing (or a device changing hands) replaces by endpoint.
	_, err := s.db.ExecContext(ctx, `INSERT INTO push_subscriptions
		(id, user_id, endpoint, p256dh, auth, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
		  user_id = excluded.user_id, p256dh = excluded.p256dh, auth = excluded.auth,
		  user_agent = excluded.user_agent, created_at = excluded.created_at`,
		sub.ID, sub.UserID, sub.Endpoint, sub.P256dh, sub.Auth, sub.UserAgent, sub.CreatedAt)
	return err
}

func (s *Store) DeletePushSubscription(ctx context.Context, userID, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions
		WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	return err
}

// DeletePushEndpoint removes a subscription the push service reported gone.
func (s *Store) DeletePushEndpoint(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

func (s *Store) PushSubscriptionsForUsers(ctx context.Context, userIDs []string) ([]*model.PushSubscription, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	q, args := inClause(userIDs)
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, endpoint, p256dh, auth, user_agent, created_at
		FROM push_subscriptions WHERE user_id IN (`+q+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.PushSubscription
	for rows.Next() {
		var p model.PushSubscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth, &p.UserAgent, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) CountPushSubscriptions(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// --- Channel mutes ---

func (s *Store) MuteChannel(ctx context.Context, userID, channelID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO channel_mutes (user_id, channel_id, created_at)
		VALUES (?, ?, ?)`, userID, channelID, now())
	return err
}

func (s *Store) UnmuteChannel(ctx context.Context, userID, channelID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM channel_mutes WHERE user_id = ? AND channel_id = ?`, userID, channelID)
	return err
}

func (s *Store) MutedChannelIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id FROM channel_mutes WHERE user_id = ?`, userID)
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

// UsersMutingChannel returns which of userIDs have muted the channel.
func (s *Store) UsersMutingChannel(ctx context.Context, channelID string, userIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(userIDs) == 0 {
		return out, nil
	}
	q, args := inClause(userIDs)
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM channel_mutes
		WHERE channel_id = ? AND user_id IN (`+q+`)`, append([]any{channelID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// inClause builds the "?, ?, ?" placeholder list and args for an IN query.
func inClause(ids []string) (string, []any) {
	q := make([]byte, 0, len(ids)*3)
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			q = append(q, ", "...)
		}
		q = append(q, '?')
		args = append(args, id)
	}
	return string(q), args
}
