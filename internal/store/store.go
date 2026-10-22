// Package store contains the persistence layer of the application.
//
// It is a small hand written data access layer that runs on SQLite and
// PostgreSQL. All queries are parameterised, so user input is never
// interpolated into SQL.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// dialect identifies the database engine behind a Store.
type dialect int

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

// Store is a handle on the application database.
//
// The same data access layer runs on SQLite (local development and the
// classic single binary deployment) and on PostgreSQL (for example Neon,
// which is used when the site runs on Vercel).
type Store struct {
	db      *sql.DB
	dialect dialect
}

// Open connects to a database. A PostgreSQL URL ("postgres://" or
// "postgresql://") selects PostgreSQL, anything else is treated as the path
// of a SQLite file.
func Open(dsn string) (*Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("store: database path must not be empty")
	}
	if isPostgresDSN(dsn) {
		db, err := openPostgres(dsn)
		if err != nil {
			return nil, err
		}
		return &Store{db: db, dialect: dialectPostgres}, nil
	}
	if strings.Contains(dsn, "://") {
		return nil, fmt.Errorf("store: unsupported database URL (want postgres:// or postgresql://)")
	}

	path := dsn
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("store: create data directory: %w", err)
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("store: resolve database path: %w", err)
	}
	url := (&url.URL{Scheme: "file", Path: abs}).String() +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", url)
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	// SQLite serialises writes anyway; a single connection keeps us safe
	// from "database is locked" errors without any busy-retry logic.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}
	return &Store{db: db, dialect: dialectSQLite}, nil
}

// Driver reports the database engine, for log lines.
func (s *Store) Driver() string {
	if s.dialect == dialectPostgres {
		return "postgres"
	}
	return "sqlite"
}

// DB exposes the underlying handle, mainly for tests.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// migration is a single forward-only schema change.
//
// A migration either runs a list of statements or a custom function, which
// is needed for changes that have to read and rewrite existing rows.
type migration struct {
	version int
	stmts   []string
	apply   func(ctx context.Context, tx *sql.Tx, d dialect) error
}

// migrationsFor returns the schema changes of a database engine.
//
// Both engines start with the same shape. The only differences are the
// syntax of automatically assigned ids and the way usernames are compared
// case-insensitively (PostgreSQL uses a unique index on lower(username)).
func migrationsFor(d dialect) []migration {
	schema := sqliteSchema
	if d == dialectPostgres {
		schema = postgresSchema
	}
	return []migration{
		{version: 1, stmts: schema},
		{
			// Converts databases that still use the four fixed option
			// columns into the new format with a variable number of
			// options. A fresh database already uses the new format and is
			// left untouched. PostgreSQL installations always start fresh,
			// so the SQLite specific table rebuild is skipped there.
			version: 2,
			apply:   migrateQuestionsToOptions,
		},
		{
			// A lesson has two texts: the Myanmar text of the lesson page
			// and the Japanese text of the question page. The new column
			// starts out empty, then the question page falls back to the
			// Myanmar text.
			version: 3,
			stmts: []string{
				`ALTER TABLE lessons ADD COLUMN quiz_passage TEXT NOT NULL DEFAULT ''`,
			},
		},
	}
}

// Migrate applies every pending schema migration.
func (s *Store) Migrate(ctx context.Context) error {
	if s.dialect == dialectPostgres {
		return s.migratePostgres(ctx)
	}
	return s.migrateSQLite(ctx)
}

// migrateSQLite applies the migrations one transaction at a time.
func (s *Store) migrateSQLite(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create migrations table: %w", err)
	}

	for _, m := range migrationsFor(dialectSQLite) {
		var exists int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("store: check migration %d: %w", m.version, err)
		}
		if exists > 0 {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", m.version, err)
		}
		if m.apply != nil {
			if err := m.apply(ctx, tx, dialectSQLite); err != nil {
				tx.Rollback()
				return fmt.Errorf("store: apply migration %d: %w", m.version, err)
			}
		}
		for _, stmt := range m.stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("store: apply migration %d: %w", m.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", m.version, err)
		}
	}
	return nil
}

// Now returns the current time in UTC, the timezone used for all timestamps.
func Now() time.Time { return time.Now().UTC() }

// execInsert runs an INSERT statement and returns the id of the new row.
//
// RETURNING is supported by SQLite and PostgreSQL, which keeps the two
// dialects on the same code path (PostgreSQL has no LastInsertId).
func (s *Store) execInsert(ctx context.Context, query string, args ...any) (int64, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx, query+` RETURNING id`, args...).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// formatTime renders a timestamp for storage.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parseTime parses a stored timestamp.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}
