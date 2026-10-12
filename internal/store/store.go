// Package store is the only package that touches SQLite. All SQL lives here.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies pending
// migrations. WAL mode, foreign keys, and a busy timeout are set via DSN.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// modernc/sqlite serializes writes; a single conn sidesteps SQLITE_BUSY
	// entirely at Phase 1 scale. Revisit with a read pool if it ever shows up
	// in profiles.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for tests and maintenance tasks (backup via VACUUM INTO).
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad version prefix", name)
		}
		if applied[version] {
			continue
		}
		sqlText, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlText)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// NewID returns a new ULID: time-ordered, so it doubles as a pagination cursor.
// ulid.Make uses a locked monotonic entropy source, keeping IDs created within
// the same millisecond in strictly increasing order (message ordering relies
// on this).
func NewID() string {
	return ulid.Make().String()
}

func now() int64 { return time.Now().UnixMilli() }

// ErrNotFound is returned by lookups that find no row.
var ErrNotFound = sql.ErrNoRows

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// scanNullStr/scanNullInt convert nullable columns back to zero-valued fields.
type nstr struct{ v *string }

func (n nstr) Scan(src any) error {
	if src == nil {
		*n.v = ""
		return nil
	}
	switch t := src.(type) {
	case string:
		*n.v = t
	case []byte:
		*n.v = string(t)
	default:
		return fmt.Errorf("nstr: unexpected %T", src)
	}
	return nil
}

type nint struct{ v *int64 }

func (n nint) Scan(src any) error {
	if src == nil {
		*n.v = 0
		return nil
	}
	switch t := src.(type) {
	case int64:
		*n.v = t
	case int:
		*n.v = int64(t)
	default:
		return fmt.Errorf("nint: unexpected %T", src)
	}
	return nil
}

// Setting helpers (server-level key/value, JSON-encoded values).

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
