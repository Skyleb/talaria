package store

import (
	"context"
	"strings"

	"github.com/skyleb/talaria/internal/model"
)

const groupCols = `id, name, icon_file_id, owner_id, created_at, updated_at`

func scanGroup(row interface{ Scan(...any) error }) (*model.Group, error) {
	var g model.Group
	err := row.Scan(&g.ID, &g.Name, nstr{&g.IconFileID}, &g.OwnerID, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// CreateGroup creates the group, owner membership, and a default channel in one tx.
func (s *Store) CreateGroup(ctx context.Context, name, ownerID string) (*model.Group, *model.Channel, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	g := &model.Group{ID: NewID(), Name: strings.TrimSpace(name), OwnerID: ownerID, CreatedAt: now(), UpdatedAt: now()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO groups (id, name, owner_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, g.ID, g.Name, g.OwnerID, g.CreatedAt, g.UpdatedAt); err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id, role, joined_at)
		VALUES (?, ?, 'owner', ?)`, g.ID, ownerID, now()); err != nil {
		return nil, nil, err
	}
	ch := &model.Channel{ID: NewID(), Kind: model.ChannelGroupText, GroupID: g.ID, Name: "general", CreatedAt: now()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO channels (id, kind, group_id, name, topic, position, created_at)
		VALUES (?, ?, ?, ?, '', 0, ?)`, ch.ID, ch.Kind, ch.GroupID, ch.Name, ch.CreatedAt); err != nil {
		return nil, nil, err
	}
	return g, ch, tx.Commit()
}

func (s *Store) GroupByID(ctx context.Context, id string) (*model.Group, error) {
	return scanGroup(s.db.QueryRowContext(ctx, `SELECT `+groupCols+` FROM groups WHERE id = ?`, id))
}

func (s *Store) UpdateGroup(ctx context.Context, id string, name, iconFileID *string) error {
	sets := []string{"updated_at = ?"}
	args := []any{now()}
	if name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*name))
	}
	if iconFileID != nil {
		sets = append(sets, "icon_file_id = ?")
		args = append(args, nullStr(*iconFileID))
	}
	args = append(args, id)
	_, err := s.db.ExecContext(ctx, `UPDATE groups SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
	return err
}

func (s *Store) ListGroupsByUser(ctx context.Context, userID string) ([]*model.Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id, g.name, g.icon_file_id, g.owner_id, g.created_at, g.updated_at
		FROM groups g JOIN group_members m ON m.group_id = g.id
		WHERE m.user_id = ? ORDER BY g.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) AddGroupMember(ctx context.Context, groupID, userID, role string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id, role, joined_at)
		VALUES (?, ?, ?, ?) ON CONFLICT(group_id, user_id) DO NOTHING`, groupID, userID, role, now())
	return err
}

func (s *Store) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID)
	return err
}

func (s *Store) GroupMember(ctx context.Context, groupID, userID string) (*model.GroupMember, error) {
	var m model.GroupMember
	err := s.db.QueryRowContext(ctx, `SELECT group_id, user_id, role, joined_at
		FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID).
		Scan(&m.GroupID, &m.UserID, &m.Role, &m.JoinedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListGroupMembers(ctx context.Context, groupID string) ([]*model.GroupMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id, user_id, role, joined_at
		FROM group_members WHERE group_id = ? ORDER BY joined_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.GroupMember
	for rows.Next() {
		var m model.GroupMember
		if err := rows.Scan(&m.GroupID, &m.UserID, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *Store) GroupMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM group_members WHERE group_id = ?`, groupID)
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

func (s *Store) CountGroupMembers(ctx context.Context, groupID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupID).Scan(&n)
	return n, err
}
