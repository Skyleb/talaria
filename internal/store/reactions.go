package store

import (
	"context"
	"errors"

	"github.com/skyleb/talaria/internal/model"
)

// MaxReactionKinds caps distinct emojis per message.
const MaxReactionKinds = 20

var ErrTooManyReactions = errors.New("too many different reactions on this message")

// AddReaction records that a user reacted with emoji (idempotent).
func (s *Store) AddReaction(ctx context.Context, messageID, userID, emoji string) error {
	var kinds int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT emoji) FROM message_reactions WHERE message_id = ?`, messageID).Scan(&kinds); err != nil {
		return err
	}
	if kinds >= MaxReactionKinds {
		var exists int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_reactions WHERE message_id = ? AND emoji = ?`, messageID, emoji).Scan(&exists)
		if exists == 0 {
			return ErrTooManyReactions
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO message_reactions (message_id, user_id, emoji, created_at)
		VALUES (?, ?, ?, ?)`, messageID, userID, emoji, now())
	return err
}

// RemoveReaction withdraws a user's reaction (idempotent).
func (s *Store) RemoveReaction(ctx context.Context, messageID, userID, emoji string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM message_reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`,
		messageID, userID, emoji)
	return err
}

// ReactionsByMessageIDs aggregates reactions per message, emojis in the order
// they were first used, user ids in reaction order.
func (s *Store) ReactionsByMessageIDs(ctx context.Context, ids []string) (map[string][]model.Reaction, error) {
	out := map[string][]model.Reaction{}
	if len(ids) == 0 {
		return out, nil
	}
	in, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx, `SELECT message_id, emoji, user_id FROM message_reactions
		WHERE message_id IN (`+in+`) ORDER BY message_id, created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := map[string]map[string]int{} // message -> emoji -> position
	for rows.Next() {
		var mid, emoji, uid string
		if err := rows.Scan(&mid, &emoji, &uid); err != nil {
			return nil, err
		}
		if index[mid] == nil {
			index[mid] = map[string]int{}
		}
		pos, ok := index[mid][emoji]
		if !ok {
			pos = len(out[mid])
			index[mid][emoji] = pos
			out[mid] = append(out[mid], model.Reaction{Emoji: emoji})
		}
		r := &out[mid][pos]
		r.Count++
		r.UserIDs = append(r.UserIDs, uid)
	}
	return out, rows.Err()
}

// HydrateReactions fills Reactions on the given messages.
func (s *Store) HydrateReactions(ctx context.Context, msgs []*model.Message) error {
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	by, err := s.ReactionsByMessageIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		m.Reactions = by[m.ID]
	}
	return nil
}
