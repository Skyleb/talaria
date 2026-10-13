package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/skyleb/talaria/internal/model"
)

// MediaItem is one attachment as shown in the media view: the attachment
// plus the channel it belongs to and how it got there.
type MediaItem struct {
	model.Attachment
	ChannelID string `json:"channel_id"`
	// Source is "message" for chat uploads and "queue" for listening-queue
	// uploads (which have no message to jump to).
	Source string `json:"source"`
}

// Media sort orders. Every order ends in the attachment id so paging is a
// total order.
const (
	MediaSortNewest  = "newest"
	MediaSortOldest  = "oldest"
	MediaSortLargest = "largest"
	MediaSortName    = "name"
)

// MediaQuery describes one page of the media view. ChannelIDs must already
// be filtered to channels the caller may read.
type MediaQuery struct {
	ChannelIDs []string
	Type       string // "", "image", "video", "audio", "file"
	Uploader   string // user id, "" = anyone
	Query      string // case-insensitive file-name substring
	Sort       string // one of the MediaSort* constants; "" = newest
	Cursor     string // opaque cursor from a previous page
	Limit      int    // 1..100, default 60
}

// ErrBadCursor is returned when a cursor is malformed or belongs to a
// different sort order than the request.
var ErrBadCursor = errors.New("invalid media cursor")

type mediaCursor struct {
	Sort string   `json:"s"`
	Keys []string `json:"k"`
}

func encodeMediaCursor(c mediaCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeMediaCursor(s, sort string) (mediaCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return mediaCursor{}, ErrBadCursor
	}
	var c mediaCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Sort != sort || len(c.Keys) == 0 {
		return mediaCursor{}, ErrBadCursor
	}
	return c, nil
}

// mediaScopedCTE selects every attachment visible through the given channels:
// attachments of live messages plus listening-queue uploads. The channel id
// list is bound twice.
const mediaScopedCTE = `WITH scoped AS (
	SELECT a.id AS id, m.channel_id AS channel_id, 'message' AS source
	  FROM attachments a JOIN messages m ON m.id = a.message_id
	 WHERE m.channel_id IN (%[1]s) AND m.deleted_at IS NULL
	UNION
	SELECT a.id, p.channel_id, 'queue'
	  FROM attachments a
	  JOIN playlist_items i ON i.upload_attachment_id = a.id
	  JOIN playlists p ON p.id = i.playlist_id
	 WHERE a.message_id IS NULL AND p.channel_id IN (%[1]s)
)`

// ListMedia returns one page of media and the cursor for the next page ("" when
// this was the last page).
func (s *Store) ListMedia(ctx context.Context, q MediaQuery) ([]MediaItem, string, error) {
	if len(q.ChannelIDs) == 0 {
		return []MediaItem{}, "", nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 60
	}
	if limit > 100 {
		limit = 100
	}
	sort := q.Sort
	if sort == "" {
		sort = MediaSortNewest
	}

	in, inArgs := inClause(q.ChannelIDs)
	var sb strings.Builder
	args := make([]any, 0, len(inArgs)*2+8)
	fmt.Fprintf(&sb, mediaScopedCTE, in)
	args = append(args, inArgs...)
	args = append(args, inArgs...)
	sb.WriteString(` SELECT ` + attCols + `, MIN(sc.channel_id), MIN(sc.source)
	  FROM scoped sc JOIN attachments a ON a.id = sc.id JOIN files f ON f.id = a.file_id
	 WHERE 1 = 1`)

	switch q.Type {
	case "image", "video", "audio":
		sb.WriteString(` AND f.mime LIKE ?`)
		args = append(args, q.Type+"/%")
	case "file":
		sb.WriteString(` AND f.mime NOT LIKE 'image/%' AND f.mime NOT LIKE 'video/%' AND f.mime NOT LIKE 'audio/%'`)
	case "":
	default:
		return nil, "", fmt.Errorf("unknown media type %q", q.Type)
	}
	if q.Uploader != "" {
		sb.WriteString(` AND a.uploader_id = ?`)
		args = append(args, q.Uploader)
	}
	if needle := strings.ToLower(strings.TrimSpace(q.Query)); needle != "" {
		sb.WriteString(` AND instr(lower(a.file_name), ?) > 0`)
		args = append(args, needle)
	}

	var order string
	switch sort {
	case MediaSortNewest:
		order = `a.id DESC`
	case MediaSortOldest:
		order = `a.id ASC`
	case MediaSortLargest:
		order = `f.size_bytes DESC, a.id DESC`
	case MediaSortName:
		order = `lower(a.file_name) ASC, a.id ASC`
	default:
		return nil, "", fmt.Errorf("unknown media sort %q", sort)
	}
	if q.Cursor != "" {
		c, err := decodeMediaCursor(q.Cursor, sort)
		if err != nil {
			return nil, "", err
		}
		switch sort {
		case MediaSortNewest:
			sb.WriteString(` AND a.id < ?`)
			args = append(args, c.Keys[0])
		case MediaSortOldest:
			sb.WriteString(` AND a.id > ?`)
			args = append(args, c.Keys[0])
		case MediaSortLargest:
			if len(c.Keys) != 2 {
				return nil, "", ErrBadCursor
			}
			size, err := strconv.ParseInt(c.Keys[0], 10, 64)
			if err != nil {
				return nil, "", ErrBadCursor
			}
			sb.WriteString(` AND (f.size_bytes < ? OR (f.size_bytes = ? AND a.id < ?))`)
			args = append(args, size, size, c.Keys[1])
		case MediaSortName:
			if len(c.Keys) != 2 {
				return nil, "", ErrBadCursor
			}
			sb.WriteString(` AND (lower(a.file_name) > ? OR (lower(a.file_name) = ? AND a.id > ?))`)
			args = append(args, c.Keys[0], c.Keys[0], c.Keys[1])
		}
	}
	sb.WriteString(` GROUP BY a.id ORDER BY ` + order + ` LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]MediaItem, 0, limit)
	for rows.Next() {
		var it MediaItem
		var w, h, d int64
		if err := rows.Scan(&it.ID, &it.FileID, nstr{&it.MessageID}, &it.UploaderID, &it.FileName,
			nint{&w}, nint{&h}, nint{&d}, &it.CreatedAt, &it.MIME, &it.SizeBytes,
			&it.ChannelID, &it.Source); err != nil {
			return nil, "", err
		}
		it.Width, it.Height, it.DurationMS = int(w), int(h), d
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		c := mediaCursor{Sort: sort}
		switch sort {
		case MediaSortLargest:
			c.Keys = []string{strconv.FormatInt(last.SizeBytes, 10), last.ID}
		case MediaSortName:
			c.Keys = []string{strings.ToLower(last.FileName), last.ID}
		default:
			c.Keys = []string{last.ID}
		}
		next = encodeMediaCursor(c)
	}
	return items, next, nil
}

// MediaUploaders lists the distinct uploader ids with media in the channels.
func (s *Store) MediaUploaders(ctx context.Context, channelIDs []string) ([]string, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	in, inArgs := inClause(channelIDs)
	args := append(append([]any{}, inArgs...), inArgs...)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(mediaScopedCTE, in)+
		` SELECT DISTINCT a.uploader_id FROM scoped sc JOIN attachments a ON a.id = sc.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
