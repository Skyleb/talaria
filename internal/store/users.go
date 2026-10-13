package store

import (
	"context"
	"strings"

	"github.com/skyle/talaria/internal/model"
)

const userCols = `id, username, display_name, password_hash, avatar_file_id,
	status_text, is_admin, created_at, updated_at, disabled_at`

func scanUser(row interface{ Scan(...any) error }) (*model.User, error) {
	var u model.User
	var isAdmin int
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash,
		nstr{&u.AvatarFileID}, &u.StatusText, &isAdmin, &u.CreatedAt, &u.UpdatedAt,
		nint{&u.DisabledAt})
	if err != nil {
		return nil, err
	}
	u.IsAdmin = isAdmin == 1
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, username, displayName, passwordHash string, isAdmin bool) (*model.User, error) {
	u := &model.User{
		ID:           NewID(),
		Username:     strings.ToLower(strings.TrimSpace(username)),
		DisplayName:  strings.TrimSpace(displayName),
		PasswordHash: passwordHash,
		IsAdmin:      isAdmin,
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}
	if u.DisplayName == "" {
		u.DisplayName = u.Username
	}
	admin := 0
	if isAdmin {
		admin = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users
		(id, username, display_name, password_hash, status_text, is_admin, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.PasswordHash, admin, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (*model.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) UserByUsername(ctx context.Context, username string) (*model.User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE username = ?`, strings.ToLower(strings.TrimSpace(username))))
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers(ctx context.Context) ([]*model.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type UserPatch struct {
	DisplayName  *string
	StatusText   *string
	AvatarFileID *string
	PasswordHash *string
	IsAdmin      *bool
	Disabled     *bool
}

func (s *Store) UpdateUser(ctx context.Context, id string, p UserPatch) error {
	sets := []string{"updated_at = ?"}
	args := []any{now()}
	if p.DisplayName != nil {
		sets = append(sets, "display_name = ?")
		args = append(args, *p.DisplayName)
	}
	if p.StatusText != nil {
		sets = append(sets, "status_text = ?")
		args = append(args, *p.StatusText)
	}
	if p.AvatarFileID != nil {
		sets = append(sets, "avatar_file_id = ?")
		args = append(args, nullStr(*p.AvatarFileID))
	}
	if p.PasswordHash != nil {
		sets = append(sets, "password_hash = ?")
		args = append(args, *p.PasswordHash)
	}
	if p.IsAdmin != nil {
		v := 0
		if *p.IsAdmin {
			v = 1
		}
		sets = append(sets, "is_admin = ?")
		args = append(args, v)
	}
	if p.Disabled != nil {
		if *p.Disabled {
			sets = append(sets, "disabled_at = ?")
			args = append(args, now())
		} else {
			sets = append(sets, "disabled_at = NULL")
		}
	}
	args = append(args, id)
	_, err := s.db.ExecContext(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// PublicUsersByIDs returns public profiles keyed by id.
func (s *Store) PublicUsersByIDs(ctx context.Context, ids []string) (map[string]model.PublicUser, error) {
	out := map[string]model.PublicUser{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, display_name, avatar_file_id, status_text
		FROM users WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u model.PublicUser
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, nstr{&u.AvatarFileID}, &u.StatusText); err != nil {
			return nil, err
		}
		out[u.ID] = u
	}
	return out, rows.Err()
}
