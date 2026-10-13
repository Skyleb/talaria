package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DDNSName is a hostname label issued by the hosted dynamic-DNS service.
type DDNSName struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	CreatedIP string `json:"created_ip"`
	LastSeen  int64  `json:"last_seen"`
	// Records currently published for the name (filled by ListDDNSNames).
	Records []DDNSRecord `json:"records,omitempty"`
}

// DDNSRecord is a DNS record the hosted service has published.
type DDNSRecord struct {
	Host      string `json:"host"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updated_at"`
}

var ErrDDNSNameTaken = errors.New("name is taken")

// CreateDDNSName reserves name with the given token hash. ErrDDNSNameTaken
// when the label is already claimed.
func (s *Store) CreateDDNSName(ctx context.Context, name, tokenHash, ip string) error {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO ddns_names
		(name, token_hash, created_at, created_ip) VALUES (?, ?, ?, ?)`,
		name, tokenHash, time.Now().UnixMilli(), ip)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDDNSNameTaken
	}
	return nil
}

// DDNSNameByToken finds the name a token hash belongs to and marks it seen.
func (s *Store) DDNSNameByToken(ctx context.Context, tokenHash string) (*DDNSName, error) {
	var n DDNSName
	err := s.db.QueryRowContext(ctx, `SELECT name, created_at, created_ip, last_seen
		FROM ddns_names WHERE token_hash = ?`, tokenHash).
		Scan(&n.Name, &n.CreatedAt, &n.CreatedIP, &n.LastSeen)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE ddns_names SET last_seen = ? WHERE name = ?`,
		time.Now().UnixMilli(), n.Name)
	return &n, nil
}

// DDNSNameExists reports whether the label is claimed.
func (s *Store) DDNSNameExists(ctx context.Context, name string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM ddns_names WHERE name = ?`, name).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// CountDDNSNames returns how many names are claimed in total.
func (s *Store) CountDDNSNames(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ddns_names`).Scan(&n)
	return n, err
}

// DeleteDDNSName removes a name and (by cascade) its records.
func (s *Store) DeleteDDNSName(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ddns_names WHERE name = ?`, name)
	return err
}

// DDNSRecord returns the published content for (host, type), or "" when none.
func (s *Store) DDNSRecord(ctx context.Context, host, typ string) (string, error) {
	var c string
	err := s.db.QueryRowContext(ctx, `SELECT content FROM ddns_records WHERE host = ? AND type = ?`,
		host, typ).Scan(&c)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return c, err
}

// PutDDNSRecord records that (host, type) now carries content.
func (s *Store) PutDDNSRecord(ctx context.Context, name, host, typ, content string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ddns_records (name, host, type, content, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(host, type) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		name, host, typ, content, time.Now().UnixMilli())
	return err
}

// DeleteDDNSRecord forgets a published record.
func (s *Store) DeleteDDNSRecord(ctx context.Context, host, typ string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ddns_records WHERE host = ? AND type = ?`, host, typ)
	return err
}

// DDNSRecordsFor lists the records published for a name.
func (s *Store) DDNSRecordsFor(ctx context.Context, name string) ([]DDNSRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT host, type, content, updated_at FROM ddns_records
		WHERE name = ? ORDER BY host, type`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DDNSRecord
	for rows.Next() {
		var r DDNSRecord
		if err := rows.Scan(&r.Host, &r.Type, &r.Content, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListDDNSNames returns every claimed name with its records, newest first.
func (s *Store) ListDDNSNames(ctx context.Context) ([]*DDNSName, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, created_at, created_ip, last_seen
		FROM ddns_names ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	var out []*DDNSName
	for rows.Next() {
		var n DDNSName
		if err := rows.Scan(&n.Name, &n.CreatedAt, &n.CreatedIP, &n.LastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, &n)
	}
	rows.Close()
	for _, n := range out {
		if n.Records, err = s.DDNSRecordsFor(ctx, n.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}
