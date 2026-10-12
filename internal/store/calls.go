package store

import "context"

// Call history persistence (Phase 2). Live call state is in-memory in rtc;
// these rows exist for history/stats only.

func (s *Store) CreateCallSession(ctx context.Context, channelID, kind, startedBy string) (string, error) {
	id := NewID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO call_sessions
		(id, channel_id, kind, started_by, started_at) VALUES (?, ?, ?, ?, ?)`,
		id, channelID, kind, startedBy, now())
	return id, err
}

func (s *Store) EndCallSession(ctx context.Context, callID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE call_sessions SET ended_at = ? WHERE id = ? AND ended_at IS NULL`, now(), callID)
	return err
}

func (s *Store) AddCallParticipant(ctx context.Context, callID, userID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO call_participants
		(call_id, user_id, joined_at) VALUES (?, ?, ?)`, callID, userID, now())
	return err
}

func (s *Store) MarkCallParticipantLeft(ctx context.Context, callID, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE call_participants SET left_at = ?
		WHERE call_id = ? AND user_id = ? AND left_at IS NULL`, now(), callID, userID)
	return err
}

// EndDanglingCalls closes any call rows left open by an unclean shutdown.
func (s *Store) EndDanglingCalls(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE call_participants SET left_at = ? WHERE left_at IS NULL`, now()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE call_sessions SET ended_at = ? WHERE ended_at IS NULL`, now())
	return err
}
