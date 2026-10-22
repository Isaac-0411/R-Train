// Package store contains the persistence layer of the application.
//
// It is a small hand written data access layer on top of SQLite. All
// queries are parameterised, so user input is never interpolated into SQL.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store is a handle on the application database.
type Store struct {
	db *sql.DB
}

// Open connects to the SQLite database at path and prepares the directory.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("store: database path must not be empty")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("store: create data directory: %w", err)
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("store: resolve database path: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: abs}).String() +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
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
	return &Store{db: db}, nil
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
	apply   func(ctx context.Context, tx *sql.Tx) error
}

var migrations = []migration{
	{
		version: 1,
		stmts: []string{
			`CREATE TABLE IF NOT EXISTS users (
				id            INTEGER PRIMARY KEY AUTOINCREMENT,
				username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
				display_name  TEXT    NOT NULL DEFAULT '',
				password_hash TEXT    NOT NULL,
				role          TEXT    NOT NULL CHECK (role IN ('admin','user')),
				is_root       INTEGER NOT NULL DEFAULT 0,
				created_at    TEXT    NOT NULL
			)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_root ON users(is_root) WHERE is_root = 1`,
			`CREATE TABLE IF NOT EXISTS sessions (
				id         TEXT    PRIMARY KEY,
				user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
				csrf_token TEXT    NOT NULL,
				flash      TEXT    NOT NULL DEFAULT '',
				flash_kind TEXT    NOT NULL DEFAULT '',
				created_at TEXT    NOT NULL,
				expires_at TEXT    NOT NULL,
				ip         TEXT    NOT NULL DEFAULT '',
				user_agent TEXT    NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at)`,
			`CREATE TABLE IF NOT EXISTS lessons (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				title      TEXT    NOT NULL,
				summary    TEXT    NOT NULL DEFAULT '',
				passage    TEXT    NOT NULL DEFAULT '',
				position   INTEGER NOT NULL DEFAULT 0,
				published  INTEGER NOT NULL DEFAULT 1,
				created_at TEXT    NOT NULL,
				updated_at TEXT    NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS questions (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				lesson_id      INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
				prompt         TEXT    NOT NULL,
				options        TEXT    NOT NULL DEFAULT '[]',
				correct_option INTEGER NOT NULL DEFAULT 1,
				position       INTEGER NOT NULL DEFAULT 0,
				created_at     TEXT    NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_questions_lesson ON questions(lesson_id, position, id)`,
			`CREATE TABLE IF NOT EXISTS attempts (
				id           INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				lesson_id    INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
				score        INTEGER NOT NULL,
				total        INTEGER NOT NULL,
				passed       INTEGER NOT NULL DEFAULT 0,
				submitted_at TEXT    NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_attempts_user ON attempts(user_id, submitted_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_attempts_lesson ON attempts(lesson_id)`,
			`CREATE TABLE IF NOT EXISTS attempt_answers (
				id              INTEGER PRIMARY KEY AUTOINCREMENT,
				attempt_id      INTEGER NOT NULL REFERENCES attempts(id) ON DELETE CASCADE,
				question_id     INTEGER NOT NULL,
				selected_option TEXT    NOT NULL,
				is_correct      INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_attempt_answers_attempt ON attempt_answers(attempt_id)`,
		},
	},
	{
		// Converts databases that still use the four fixed option columns
		// into the new format with a variable number of options. A fresh
		// database already uses the new format and is left untouched.
		version: 2,
		apply:   migrateQuestionsToOptions,
	},
	{
		// A lesson has two texts: the Myanmar text of the lesson page and
		// the Japanese text of the question page. The new column starts out
		// empty, then the question page falls back to the Myanmar text.
		version: 3,
		stmts: []string{
			`ALTER TABLE lessons ADD COLUMN quiz_passage TEXT NOT NULL DEFAULT ''`,
		},
	},
}

// Migrate applies every pending schema migration.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create migrations table: %w", err)
	}

	for _, m := range migrations {
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
			if err := m.apply(ctx, tx); err != nil {
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
// The driver does not support scanning the result of a plain INSERT, so the
// id is read from the driver's LastInsertId instead.
func (s *Store) execInsert(ctx context.Context, query string, args ...any) (int64, error) {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: last insert id: %w", err)
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
