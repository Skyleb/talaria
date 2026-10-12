package store

import (
	"context"
	"strings"

	"github.com/skyle/lantern/internal/model"
)

func (s *Store) CreateFile(ctx context.Context, f *model.File) error {
	if f.ID == "" {
		f.ID = NewID()
	}
	f.CreatedAt = now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO files (id, sha256, size_bytes, mime, created_at)
		VALUES (?, ?, ?, ?, ?)`, f.ID, f.SHA256, f.SizeBytes, f.MIME, f.CreatedAt)
	return err
}

func (s *Store) FileByID(ctx context.Context, id string) (*model.File, error) {
	var f model.File
	err := s.db.QueryRowContext(ctx, `SELECT id, sha256, size_bytes, mime, created_at
		FROM files WHERE id = ?`, id).Scan(&f.ID, &f.SHA256, &f.SizeBytes, &f.MIME, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// FileBySHA returns an existing file row for a hash (dedup path).
func (s *Store) FileBySHA(ctx context.Context, sha string) (*model.File, error) {
	var f model.File
	err := s.db.QueryRowContext(ctx, `SELECT id, sha256, size_bytes, mime, created_at
		FROM files WHERE sha256 = ? LIMIT 1`, sha).Scan(&f.ID, &f.SHA256, &f.SizeBytes, &f.MIME, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

const attCols = `a.id, a.file_id, a.message_id, a.uploader_id, a.file_name,
	a.width, a.height, a.duration_ms, a.created_at, f.mime, f.size_bytes`

func scanAttachment(row interface{ Scan(...any) error }) (*model.Attachment, error) {
	var a model.Attachment
	var w, h, d int64
	err := row.Scan(&a.ID, &a.FileID, nstr{&a.MessageID}, &a.UploaderID, &a.FileName,
		nint{&w}, nint{&h}, nint{&d}, &a.CreatedAt, &a.MIME, &a.SizeBytes)
	if err != nil {
		return nil, err
	}
	a.Width, a.Height, a.DurationMS = int(w), int(h), d
	return &a, nil
}

func (s *Store) CreateAttachment(ctx context.Context, a *model.Attachment) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	a.CreatedAt = now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO attachments
		(id, file_id, uploader_id, file_name, width, height, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.FileID, a.UploaderID, a.FileName,
		nullInt(int64(a.Width)), nullInt(int64(a.Height)), nullInt(a.DurationMS), a.CreatedAt)
	return err
}

func (s *Store) AttachmentByID(ctx context.Context, id string) (*model.Attachment, error) {
	return scanAttachment(s.db.QueryRowContext(ctx, `SELECT `+attCols+`
		FROM attachments a JOIN files f ON f.id = a.file_id WHERE a.id = ?`, id))
}

func (s *Store) AttachmentsByMessageIDs(ctx context.Context, msgIDs []string) (map[string][]model.Attachment, error) {
	out := map[string][]model.Attachment{}
	if len(msgIDs) == 0 {
		return out, nil
	}
	ph := strings.Repeat("?,", len(msgIDs))
	ph = ph[:len(ph)-1]
	args := make([]any, len(msgIDs))
	for i, id := range msgIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+attCols+`
		FROM attachments a JOIN files f ON f.id = a.file_id
		WHERE a.message_id IN (`+ph+`) ORDER BY a.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out[a.MessageID] = append(out[a.MessageID], *a)
	}
	return out, rows.Err()
}

// DeleteStaleOrphanAttachments removes pending attachments older than cutoff
// and returns file ids that are now fully unreferenced (candidates for blob GC).
func (s *Store) DeleteStaleOrphanAttachments(ctx context.Context, cutoff int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM attachments WHERE message_id IS NULL AND created_at < ?`, cutoff); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, sha256 FROM files WHERE
		id NOT IN (SELECT file_id FROM attachments)
		AND id NOT IN (SELECT avatar_file_id FROM users WHERE avatar_file_id IS NOT NULL)
		AND id NOT IN (SELECT icon_file_id FROM groups WHERE icon_file_id IS NOT NULL)`)
	if err != nil {
		return nil, err
	}
	type rec struct{ id, sha string }
	var dead []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.sha); err != nil {
			rows.Close()
			return nil, err
		}
		dead = append(dead, r)
	}
	rows.Close()
	var orphanSHAs []string
	for _, r := range dead {
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, r.id); err != nil {
			return nil, err
		}
		// Only report the blob as deletable if no other file row shares the hash.
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE sha256 = ?`, r.sha).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			orphanSHAs = append(orphanSHAs, r.sha)
		}
	}
	return orphanSHAs, tx.Commit()
}
