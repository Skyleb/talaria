package store

import (
	"context"
	"strings"

	"github.com/skyle/talaria/internal/model"
)

// SearchHit is a message plus the FTS snippet with match markers.
type SearchHit struct {
	Message *model.Message `json:"message"`
	Snippet string         `json:"snippet"`
}

// Snippet match markers; the client turns these into highlights.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// BuildFTSQuery turns free text into an FTS5 query: every word becomes a
// quoted prefix term, all required. Returns "" when nothing searchable.
func BuildFTSQuery(text string) string {
	var terms []string
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, `"'`)
		w = strings.ReplaceAll(w, `"`, ``)
		if w == "" {
			continue
		}
		terms = append(terms, `"`+w+`"*`)
	}
	return strings.Join(terms, " ")
}

// VisibleChannelIDs lists every channel the user may read: channels of
// groups they belong to plus DMs they participate in.
func (s *Store) VisibleChannelIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id FROM channels c JOIN group_members gm ON gm.group_id = c.group_id WHERE gm.user_id = ?
		UNION
		SELECT channel_id FROM dm_participants WHERE user_id = ?`, userID, userID)
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

// SearchMessages runs an FTS query restricted to channelIDs, newest first,
// paging with before (a message id). limit+1 rows are fetched to report more.
func (s *Store) SearchMessages(ctx context.Context, ftsQuery string, channelIDs []string, before string, limit int) ([]SearchHit, bool, error) {
	if ftsQuery == "" || len(channelIDs) == 0 {
		return []SearchHit{}, false, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	in, args := inClause(channelIDs)
	q := `SELECT m.` + strings.ReplaceAll(msgCols, ", ", ", m.") + `,
		snippet(f, 0, ?, ?, '…', 14)
		FROM messages_fts f JOIN messages m ON m.id = f.message_id
		WHERE messages_fts MATCH ? AND f.channel_id IN (` + in + `) AND m.deleted_at IS NULL`
	all := append([]any{SnippetOpen, SnippetClose, ftsQuery}, args...)
	if before != "" {
		q += ` AND m.id < ?`
		all = append(all, before)
	}
	q += ` ORDER BY m.id DESC LIMIT ?`
	all = append(all, limit+1)
	rows, err := s.db.QueryContext(ctx, q, all...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []SearchHit{}
	for rows.Next() {
		var m model.Message
		var mentions, snippet string
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.AuthorID, &m.Kind, &m.Content,
			nstr{&m.ReplyToID}, &m.CreatedAt, nint{&m.EditedAt}, nint{&m.DeletedAt}, &mentions, &snippet); err != nil {
			return nil, false, err
		}
		if mentions != "" {
			m.MentionIDs = strings.Split(mentions, ",")
		}
		out = append(out, SearchHit{Message: &m, Snippet: snippet})
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, rows.Err()
}

// ListMessagesAfter returns up to limit messages newer than after, oldest
// first in the slice (callers reverse as needed), plus whether more follow.
func (s *Store) ListMessagesAfter(ctx context.Context, channelID, after string, limit int) ([]*model.Message, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages
		WHERE channel_id = ? AND id > ? ORDER BY id ASC LIMIT ?`, channelID, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []*model.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, m)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, rows.Err()
}

// ListMessagesAround returns a window centred on target: up to half older
// (including target) and half newer, oldest first. The booleans report
// whether more exist before and after the window.
func (s *Store) ListMessagesAround(ctx context.Context, channelID, target string, limit int) ([]*model.Message, bool, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	half := limit / 2
	// Older half: id <= target, newest first, then reversed.
	rows, err := s.db.QueryContext(ctx, `SELECT `+msgCols+` FROM messages
		WHERE channel_id = ? AND id <= ? ORDER BY id DESC LIMIT ?`, channelID, target, half+1)
	if err != nil {
		return nil, false, false, err
	}
	var before []*model.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, false, false, err
		}
		before = append(before, m)
	}
	rows.Close()
	moreBefore := len(before) > half
	if moreBefore {
		before = before[:half]
	}
	for i, j := 0, len(before)-1; i < j; i, j = i+1, j-1 {
		before[i], before[j] = before[j], before[i]
	}
	after, moreAfter, err := s.ListMessagesAfter(ctx, channelID, target, half)
	if err != nil {
		return nil, false, false, err
	}
	return append(before, after...), moreBefore, moreAfter, nil
}
