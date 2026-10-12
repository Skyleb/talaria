package store

import (
	"context"

	"github.com/skyle/lantern/internal/model"
)

func (s *Store) CreateSession(ctx context.Context, sess *model.Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
		(id, user_id, created_at, expires_at, last_seen_at, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.CreatedAt, sess.ExpiresAt, sess.LastSeenAt, sess.UserAgent, sess.IP)
	return err
}

func (s *Store) SessionByID(ctx context.Context, id string) (*model.Session, error) {
	var sess model.Session
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, created_at, expires_at, last_seen_at, user_agent, ip
		FROM sessions WHERE id = ?`, id).
		Scan(&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt, &sess.LastSeenAt, &sess.UserAgent, &sess.IP)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, id string, lastSeen, expires int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, lastSeen, expires, id)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, cutoff int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, cutoff)
	return err
}

func (s *Store) ListSessionsByUser(ctx context.Context, userID string) ([]*model.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, created_at, expires_at, last_seen_at, user_agent, ip
		FROM sessions WHERE user_id = ? ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Session
	for rows.Next() {
		var sess model.Session
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt,
			&sess.LastSeenAt, &sess.UserAgent, &sess.IP); err != nil {
			return nil, err
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}
