package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"

	"github.com/skyle/lantern/internal/model"
)

// newInviteCode returns a 10-char Crockford-base32 code (no ambiguous chars).
func newInviteCode() string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func (s *Store) CreateInvite(ctx context.Context, kind, groupID, createdBy string, maxUses, expiresAt int64) (*model.Invite, error) {
	inv := &model.Invite{
		Code: newInviteCode(), Kind: kind, GroupID: groupID, CreatedBy: createdBy,
		MaxUses: maxUses, ExpiresAt: expiresAt, CreatedAt: now(),
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO invites
		(code, kind, group_id, created_by, max_uses, use_count, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		inv.Code, inv.Kind, nullStr(inv.GroupID), inv.CreatedBy,
		nullInt(inv.MaxUses), nullInt(inv.ExpiresAt), inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

const inviteCols = `code, kind, group_id, created_by, max_uses, use_count, expires_at, revoked_at, created_at`

func scanInvite(row interface{ Scan(...any) error }) (*model.Invite, error) {
	var inv model.Invite
	err := row.Scan(&inv.Code, &inv.Kind, nstr{&inv.GroupID}, &inv.CreatedBy,
		nint{&inv.MaxUses}, &inv.UseCount, nint{&inv.ExpiresAt}, nint{&inv.RevokedAt}, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (s *Store) InviteByCode(ctx context.Context, code string) (*model.Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM invites WHERE code = ?`, code))
}

var ErrInviteInvalid = errors.New("invite invalid or exhausted")

// ConsumeInvite atomically validates and increments use_count.
func (s *Store) ConsumeInvite(ctx context.Context, code string) (*model.Invite, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE invites SET use_count = use_count + 1
		WHERE code = ? AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > ?)
		  AND (max_uses IS NULL OR use_count < max_uses)`, code, now())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrInviteInvalid
	}
	return s.InviteByCode(ctx, code)
}

func (s *Store) RevokeInvite(ctx context.Context, code string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE invites SET revoked_at = ? WHERE code = ? AND revoked_at IS NULL`, now(), code)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListInvites(ctx context.Context, kind, groupID string) ([]*model.Invite, error) {
	var rows *sql.Rows
	var err error
	if groupID != "" {
		rows, err = s.db.QueryContext(ctx, `SELECT `+inviteCols+` FROM invites
			WHERE kind = ? AND group_id = ? ORDER BY created_at DESC`, kind, groupID)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT `+inviteCols+` FROM invites
			WHERE kind = ? ORDER BY created_at DESC`, kind)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}
