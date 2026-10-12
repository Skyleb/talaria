package store

import (
	"context"
	"database/sql"

	"github.com/skyle/talaria/internal/model"
)

func pairKey(a, b string) string {
	if a < b {
		return a + ":" + b
	}
	return b + ":" + a
}

const friendCols = `id, requester_id, addressee_id, status, created_at, updated_at`

func scanFriendship(row interface{ Scan(...any) error }) (*model.Friendship, error) {
	var f model.Friendship
	err := row.Scan(&f.ID, &f.RequesterID, &f.AddresseeID, &f.Status, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// FriendshipBetween returns the (single) row for a user pair, or ErrNotFound.
func (s *Store) FriendshipBetween(ctx context.Context, a, b string) (*model.Friendship, error) {
	return scanFriendship(s.db.QueryRowContext(ctx,
		`SELECT `+friendCols+` FROM friendships WHERE pair_key = ?`, pairKey(a, b)))
}

func (s *Store) FriendshipByID(ctx context.Context, id string) (*model.Friendship, error) {
	return scanFriendship(s.db.QueryRowContext(ctx,
		`SELECT `+friendCols+` FROM friendships WHERE id = ?`, id))
}

// CreateFriendRequest inserts a pending request. A previously declined/removed
// pair (row deleted) can request again; blocked pairs are rejected by callers
// who check FriendshipBetween first.
func (s *Store) CreateFriendRequest(ctx context.Context, requesterID, addresseeID string) (*model.Friendship, error) {
	f := &model.Friendship{
		ID: NewID(), RequesterID: requesterID, AddresseeID: addresseeID,
		Status: model.FriendPending, CreatedAt: now(), UpdatedAt: now(),
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO friendships
		(id, requester_id, addressee_id, status, created_at, updated_at, pair_key)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.RequesterID, f.AddresseeID, f.Status, f.CreatedAt, f.UpdatedAt,
		pairKey(requesterID, addresseeID))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Store) UpdateFriendshipStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE friendships SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteFriendship(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM friendships WHERE id = ?`, id)
	return err
}

// ReplaceFriendshipAsBlock deletes any pair row and inserts a block owned by blocker.
func (s *Store) ReplaceFriendshipAsBlock(ctx context.Context, blockerID, blockedID string) (*model.Friendship, error) {
	key := pairKey(blockerID, blockedID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM friendships WHERE pair_key = ?`, key); err != nil {
		return nil, err
	}
	f := &model.Friendship{
		ID: NewID(), RequesterID: blockerID, AddresseeID: blockedID,
		Status: model.FriendBlocked, CreatedAt: now(), UpdatedAt: now(),
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO friendships
		(id, requester_id, addressee_id, status, created_at, updated_at, pair_key)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.RequesterID, f.AddresseeID, f.Status, f.CreatedAt, f.UpdatedAt, key); err != nil {
		return nil, err
	}
	return f, tx.Commit()
}

// ListFriendships returns all rows involving the user (any status).
func (s *Store) ListFriendships(ctx context.Context, userID string) ([]*model.Friendship, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+friendCols+` FROM friendships
		WHERE requester_id = ? OR addressee_id = ? ORDER BY updated_at DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Friendship
	for rows.Next() {
		f, err := scanFriendship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AreFriends reports whether the pair has an accepted friendship.
func (s *Store) AreFriends(ctx context.Context, a, b string) (bool, error) {
	f, err := s.FriendshipBetween(ctx, a, b)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return f.Status == model.FriendAccepted, nil
}
