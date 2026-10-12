package store

import (
	"context"
	"strings"

	"github.com/skyle/lantern/internal/model"
)

const msgCols = `id, channel_id, author_id, kind, content, reply_to_id, created_at, edited_at, deleted_at`

func scanMessage(row interface{ Scan(...any) error }) (*model.Message, error) {
	var m model.Message
	err := row.Scan(&m.ID, &m.ChannelID, &m.AuthorID, &m.Kind, &m.Content,
		nstr{&m.ReplyToID}, &m.CreatedAt, nint{&m.EditedAt}, nint{&m.DeletedAt})
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) CreateMessage(ctx context.Context, m *model.Message, attachmentIDs []string) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	m.CreatedAt = now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages
		(id, channel_id, author_id, kind, content, reply_to_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ChannelID, m.AuthorID, m.Kind, m.Content, nullStr(m.ReplyToID), m.CreatedAt); err != nil {
		return err
	}
	for _, aid := range attachmentIDs {
		// Only the uploader's own pending attachments may be claimed.
		res, err := tx.ExecContext(ctx, `UPDATE attachments SET message_id = ?
			WHERE id = ? AND uploader_id = ? AND message_id IS NULL`, m.ID, aid, m.AuthorID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit()
}

// MessageByID returns the message with attachments (no author hydration).
func (s *Store) MessageByID(ctx context.Context, id string) (*model.Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	atts, err := s.AttachmentsByMessageIDs(ctx, []string{m.ID})
	if err != nil {
		return nil, err
	}
	m.Attachments = atts[m.ID]
	return m, nil
}

// ListMessages pages a channel descending by ID (ULID = time order).
// before="" starts at the newest.
func (s *Store) ListMessages(ctx context.Context, channelID, before string, limit int) ([]*model.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var rows interface {
		Next() bool
		Scan(...any) error
		Close() error
		Err() error
	}
	var err error
	if before == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages
			WHERE channel_id = ? ORDER BY id DESC LIMIT ?`, channelID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages
			WHERE channel_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, channelID, before, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Message
	ids := []string{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	atts, err := s.AttachmentsByMessageIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, m := range out {
		m.Attachments = atts[m.ID]
	}
	return out, nil
}

func (s *Store) UpdateMessageContent(ctx context.Context, id, content string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE messages SET content = ?, edited_at = ? WHERE id = ? AND deleted_at IS NULL`,
		content, now(), id)
	return err
}

// SoftDeleteMessage blanks content and detaches attachments.
func (s *Store) SoftDeleteMessage(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE messages SET content = '', deleted_at = ? WHERE id = ?`, now(), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE message_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// LatestMessageIDs returns the newest message id per channel.
func (s *Store) LatestMessageIDs(ctx context.Context, channelIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(channelIDs) == 0 {
		return out, nil
	}
	ph := strings.Repeat("?,", len(channelIDs))
	ph = ph[:len(ph)-1]
	args := make([]any, len(channelIDs))
	for i, id := range channelIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id, MAX(id) FROM messages
		WHERE channel_id IN (`+ph+`) GROUP BY channel_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, mid string
		if err := rows.Scan(&cid, &mid); err != nil {
			return nil, err
		}
		out[cid] = mid
	}
	return out, rows.Err()
}

func (s *Store) UpsertReadState(ctx context.Context, userID, channelID, lastReadMessageID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO read_states (user_id, channel_id, last_read_message_id, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, channel_id) DO UPDATE SET
		  last_read_message_id = excluded.last_read_message_id, updated_at = excluded.updated_at`,
		userID, channelID, lastReadMessageID, now())
	return err
}

func (s *Store) ListReadStates(ctx context.Context, userID string) ([]*model.ReadState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id, last_read_message_id, updated_at
		FROM read_states WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ReadState
	for rows.Next() {
		var r model.ReadState
		if err := rows.Scan(&r.ChannelID, nstr{&r.LastReadMessageID}, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
