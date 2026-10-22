package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"readingtrainer/internal/auth"
)

// Session is a browser session backed by a database row.
//
// Sessions for anonymous visitors exist as well: they carry no user and are
// only used to hold the CSRF token, which keeps a single mechanism for
// protecting every form on the site.
type Session struct {
	ID        string
	UserID    int64
	HasUser   bool
	CSRFToken string
	Flash     string
	FlashKind string
	CreatedAt time.Time
	ExpiresAt time.Time
	IP        string
	UserAgent string
}

// CreateSession stores a new session. Pass userID 0 for a guest session.
func (s *Store) CreateSession(ctx context.Context, userID int64, ip, userAgent string, ttl time.Duration) (*Session, error) {
	id, err := auth.RandomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := auth.RandomToken(32)
	if err != nil {
		return nil, err
	}

	now := Now()
	session := &Session{
		ID:        id,
		UserID:    userID,
		HasUser:   userID > 0,
		CSRFToken: csrf,
		CreatedAt: now,
		// Stored timestamps have a precision of one second, so the value
		// handed to the cookie is truncated the same way.
		ExpiresAt: now.Add(ttl).Truncate(time.Second),
		IP:        ip,
		UserAgent: truncate(userAgent, 255),
	}

	var uid any
	if userID > 0 {
		uid = userID
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, csrf_token, flash, flash_kind, created_at, expires_at, ip, user_agent)
		 VALUES (?, ?, ?, '', '', ?, ?, ?, ?)`,
		session.ID, uid, session.CSRFToken, formatTime(session.CreatedAt),
		formatTime(session.ExpiresAt), session.IP, session.UserAgent)
	if err != nil {
		return nil, fmt.Errorf("store: insert session: %w", err)
	}
	return session, nil
}

// GetSession loads a session by its token. Expired sessions are treated as
// missing and are removed lazily.
func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, csrf_token, flash, flash_kind, created_at, expires_at, ip, user_agent
		 FROM sessions WHERE id = ?`, id)

	var (
		session   Session
		userID    sql.NullInt64
		createdAt string
		expiresAt string
	)
	err := row.Scan(&session.ID, &userID, &session.CSRFToken, &session.Flash, &session.FlashKind,
		&createdAt, &expiresAt, &session.IP, &session.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get session: %w", err)
	}

	session.HasUser = userID.Valid && userID.Int64 > 0
	session.UserID = userID.Int64
	if c, err := parseTime(createdAt); err == nil {
		session.CreatedAt = c
	}
	if e, err := parseTime(expiresAt); err == nil {
		session.ExpiresAt = e
	}
	return &session, nil
}

// TouchSession extends the lifetime of a session and returns the new expiry.
func (s *Store) TouchSession(ctx context.Context, id string, ttl time.Duration) (time.Time, error) {
	expires := Now().Add(ttl).Truncate(time.Second)
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE id = ?`, formatTime(expires), id); err != nil {
		return time.Time{}, fmt.Errorf("store: touch session: %w", err)
	}
	return expires, nil
}

// DeleteSession removes a session, e.g. on logout.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions removes every session of a user.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete user sessions: %w", err)
	}
	return nil
}

// SetFlash stores a one-shot message that is shown on the next rendered page.
func (s *Store) SetFlash(ctx context.Context, id, kind, message string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET flash = ?, flash_kind = ? WHERE id = ?`,
		truncate(message, 500), kind, id); err != nil {
		return fmt.Errorf("store: set flash: %w", err)
	}
	return nil
}

// TakeFlash returns and clears the stored flash message.
func (s *Store) TakeFlash(ctx context.Context, id string) (kind, message string) {
	if id == "" {
		return "", ""
	}
	var flash, flashKind string
	err := s.db.QueryRowContext(ctx, `SELECT flash, flash_kind FROM sessions WHERE id = ?`, id).Scan(&flash, &flashKind)
	if err != nil {
		return "", ""
	}
	if flash == "" {
		return "", ""
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET flash = '', flash_kind = '' WHERE id = ?`, id); err != nil {
		return flashKind, flash
	}
	return flashKind, flash
}

// DeleteExpiredSessions removes sessions whose lifetime has elapsed.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, formatTime(Now()))
	if err != nil {
		return 0, fmt.Errorf("store: prune sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// RotateSession replaces a session with a brand new one, keeping the user
// signed in. It is used right after a successful login to prevent session
// fixation attacks.
func (s *Store) RotateSession(ctx context.Context, oldID string, userID int64, ip, userAgent string, ttl time.Duration) (*Session, error) {
	if err := s.DeleteSession(ctx, oldID); err != nil {
		return nil, err
	}
	return s.CreateSession(ctx, userID, ip, userAgent, ttl)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
