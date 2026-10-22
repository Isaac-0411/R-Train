package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

// The data access layer writes SQLite style "?" placeholders. PostgreSQL
// only understands "$1", "$2", …, so the pgx driver is wrapped in a thin
// adapter that rewrites the placeholders before the query reaches the
// server. Keeping one placeholder style means the SQL in the store works on
// both engines without a second set of queries.
const postgresDriverName = "pgx-question-mark"

var registerPostgresDriver sync.Once

// isPostgresDSN reports whether dsn points at a PostgreSQL server.
func isPostgresDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// openPostgres connects to PostgreSQL. The connection string can be a Neon
// pooled URL; the pool stays deliberately small because every serverless
// instance creates its own.
func openPostgres(dsn string) (*sql.DB, error) {
	registerPostgresDriver.Do(func() {
		sql.Register(postgresDriverName, rewriteDriver{inner: stdlib.GetDefaultDriver()})
	})

	db, err := sql.Open(postgresDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open postgres: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping postgres: %w", err)
	}
	return db, nil
}

// migrationLockKey is an arbitrary application specific advisory lock id.
const migrationLockKey int64 = 0x52545241494e // "RTRAIN"

// migratePostgres applies the migrations inside a single transaction.
//
// A transaction scoped advisory lock serialises concurrent cold starts, and
// unlike a session level lock it also works through a transaction pooling
// proxy such as the Neon pooled endpoint. PostgreSQL DDL is transactional,
// so a failed migration rolls back completely.
func (s *Store) migratePostgres(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin migrations: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(?)`, migrationLockKey); err != nil {
		return fmt.Errorf("store: lock migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create migrations table: %w", err)
	}

	for _, m := range migrationsFor(dialectPostgres) {
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&exists); err != nil {
			return fmt.Errorf("store: check migration %d: %w", m.version, err)
		}
		if exists > 0 {
			continue
		}
		if m.apply != nil {
			if err := m.apply(ctx, tx, dialectPostgres); err != nil {
				return fmt.Errorf("store: apply migration %d: %w", m.version, err)
			}
		}
		for _, stmt := range m.stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("store: apply migration %d: %w", m.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("store: record migration %d: %w", m.version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migrations: %w", err)
	}
	return nil
}

// rewriteDriver wraps another driver and rewrites "?" placeholders.
type rewriteDriver struct {
	inner driver.Driver
}

func (d rewriteDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &rewriteConn{Conn: conn}, nil
}

// rewriteConn forwards every operation, rewriting the query of the ones that
// carry SQL. Optional driver interfaces are forwarded when the wrapped
// connection supports them; otherwise database/sql falls back to preparing
// the statement, which is rewritten as well.
type rewriteConn struct {
	driver.Conn
}

func (c *rewriteConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(rewritePlaceholders(query))
	if err != nil {
		return nil, err
	}
	return &rewriteStmt{Stmt: stmt}, nil
}

func (c *rewriteConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err := preparer.PrepareContext(ctx, rewritePlaceholders(query))
		if err != nil {
			return nil, err
		}
		return &rewriteStmt{Stmt: stmt}, nil
	}
	return c.Prepare(query)
}

func (c *rewriteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := c.Conn.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, rewritePlaceholders(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *rewriteConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, rewritePlaceholders(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *rewriteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

func (c *rewriteConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *rewriteConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *rewriteConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

// rewriteStmt forwards to an already rewritten prepared statement.
type rewriteStmt struct {
	driver.Stmt
}

func (s *rewriteStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := s.Stmt.(driver.StmtExecContext); ok {
		return execer.ExecContext(ctx, args)
	}
	return nil, driver.ErrSkip
}

func (s *rewriteStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return queryer.QueryContext(ctx, args)
	}
	return nil, driver.ErrSkip
}

// rewritePlaceholders turns each "?" outside of strings and comments into
// "$n" with n counting from one.
func rewritePlaceholders(query string) string {
	if !strings.ContainsRune(query, '?') {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 8)
	param := 0

	for i := 0; i < len(query); i++ {
		switch c := query[i]; c {
		case '\'', '"':
			// Copy a quoted string or identifier, including the SQL
			// escaping of doubled quotes.
			b.WriteByte(c)
			i++
			for i < len(query) {
				b.WriteByte(query[i])
				if query[i] == c {
					if i+1 < len(query) && query[i+1] == c {
						i++
						b.WriteByte(query[i])
						i++
						continue
					}
					break
				}
				i++
			}
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				for i < len(query) && query[i] != '\n' {
					b.WriteByte(query[i])
					i++
				}
				if i < len(query) {
					b.WriteByte(query[i])
				}
				continue
			}
			b.WriteByte(c)
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				b.WriteString("/*")
				i += 2
				for i < len(query) {
					if query[i] == '*' && i+1 < len(query) && query[i+1] == '/' {
						b.WriteString("*/")
						i++
						break
					}
					b.WriteByte(query[i])
					i++
				}
				continue
			}
			b.WriteByte(c)
		case '?':
			param++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(param))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
