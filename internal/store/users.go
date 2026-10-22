package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when a lookup does not match any row.
var ErrNotFound = errors.New("store: record not found")

// ErrDuplicateUsername is returned when a username is already taken.
var ErrDuplicateUsername = errors.New("store: username already exists")

// User is an account of the application.
type User struct {
	ID           int64
	Username     string
	DisplayName  string
	PasswordHash string
	Role         string
	IsRoot       bool
	CreatedAt    time.Time
}

// IsAdmin reports whether the user has administrative privileges.
func (u *User) IsAdmin() bool { return u != nil && u.Role == RoleAdmin }

// FullName returns the display name, falling back to the username.
func (u *User) FullName() string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	return u.Username
}

// Role names used across the application.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

const userColumns = `id, username, display_name, password_hash, role, is_root, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var (
		u         User
		root      int
		createdAt string
	)
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &root, &createdAt); err != nil {
		return nil, err
	}
	u.IsRoot = root == 1
	parsed, err := parseTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("store: parse created_at: %w", err)
	}
	u.CreatedAt = parsed
	return &u, nil
}

// CreateUser inserts a new account.
//
// When isRoot is true the account becomes the absolute administrator; only
// one root account may exist, which the database enforces with a unique
// index.
func (s *Store) CreateUser(ctx context.Context, username, displayName, passwordHash, role string, isRoot bool) (*User, error) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if role != RoleAdmin && role != RoleUser {
		return nil, fmt.Errorf("store: invalid role %q", role)
	}

	rootValue := 0
	if isRoot {
		rootValue = 1
	}

	var id int64
	id, err := s.execInsert(ctx,
		`INSERT INTO users (username, display_name, password_hash, role, is_root, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		username, displayName, passwordHash, role, rootValue, formatTime(Now()))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateUsername
		}
		return nil, fmt.Errorf("store: insert user: %w", err)
	}
	return s.GetUserByID(ctx, id)
}

// GetUserByID loads an account by its identifier.
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get user by id: %w", err)
	}
	return u, nil
}

// GetUserByUsername loads an account by its username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	// lower() works on both engines and matches the unique index of
	// PostgreSQL (SQLite additionally compares the column with NOCASE).
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE lower(username) = lower(?)`,
		strings.TrimSpace(username))
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get user by username: %w", err)
	}
	return u, nil
}

// ListUsers returns every account ordered by creation time.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY is_root DESC, created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan user: %w", err)
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// HasRootAdmin reports whether an absolute administrator already exists.
func (s *Store) HasRootAdmin(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_root = 1`).Scan(&count); err != nil {
		return false, fmt.Errorf("store: check root admin: %w", err)
	}
	return count > 0, nil
}

// UpdateUserProfile changes the display name of an account.
func (s *Store) UpdateUserProfile(ctx context.Context, id int64, displayName string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET display_name = ? WHERE id = ?`,
		strings.TrimSpace(displayName), id)
	if err != nil {
		return fmt.Errorf("store: update user profile: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserPassword replaces the stored password hash of an account.
func (s *Store) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return fmt.Errorf("store: update password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserRole changes the role of an account. The root administrator's
// role can never be changed.
func (s *Store) UpdateUserRole(ctx context.Context, id int64, role string) error {
	if role != RoleAdmin && role != RoleUser {
		return fmt.Errorf("store: invalid role %q", role)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ? AND is_root = 0`, role, id)
	if err != nil {
		return fmt.Errorf("store: update role: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes an account and everything that belongs to it
// (sessions and attempts) through cascading foreign keys.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND is_root = 0`, id)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed")
}
