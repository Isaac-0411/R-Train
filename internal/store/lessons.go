package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Lesson is a reading lesson with its Myanmar text and its question set.
type Lesson struct {
	ID        int64
	Title     string
	Summary   string
	Passage   string
	Position  int
	Published bool
	CreatedAt time.Time
	UpdatedAt time.Time
	// QuestionCount is filled in by list queries that need it.
	QuestionCount int
}

const lessonColumns = `id, title, summary, passage, position, published, created_at, updated_at`

func scanLesson(row interface{ Scan(...any) error }) (*Lesson, error) {
	var (
		l         Lesson
		published int
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&l.ID, &l.Title, &l.Summary, &l.Passage, &l.Position, &published, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	l.Published = published == 1
	if c, err := parseTime(createdAt); err == nil {
		l.CreatedAt = c
	}
	if u, err := parseTime(updatedAt); err == nil {
		l.UpdatedAt = u
	}
	return &l, nil
}

// CreateLesson inserts a new lesson.
func (s *Store) CreateLesson(ctx context.Context, title, summary, passage string, published bool) (*Lesson, error) {
	now := Now()
	pub := 0
	if published {
		pub = 1
	}
	var position int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM lessons`).Scan(&position); err != nil {
		return nil, fmt.Errorf("store: next lesson position: %w", err)
	}

	var id int64
	id, err := s.execInsert(ctx,
		`INSERT INTO lessons (title, summary, passage, position, published, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(title), strings.TrimSpace(summary), passage, position, pub,
		formatTime(now), formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("store: insert lesson: %w", err)
	}
	return s.GetLesson(ctx, id)
}

// GetLesson loads a lesson by its identifier.
func (s *Store) GetLesson(ctx context.Context, id int64) (*Lesson, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+lessonColumns+` FROM lessons WHERE id = ?`, id)
	l, err := scanLesson(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get lesson: %w", err)
	}
	return l, nil
}

// ListLessons returns all lessons with their question counts.
func (s *Store) ListLessons(ctx context.Context) ([]Lesson, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.title, l.summary, l.passage, l.position, l.published, l.created_at, l.updated_at,
		        (SELECT COUNT(*) FROM questions q WHERE q.lesson_id = l.id)
		 FROM lessons l ORDER BY l.position ASC, l.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list lessons: %w", err)
	}
	defer rows.Close()

	lessons := []Lesson{}
	for rows.Next() {
		var (
			l         Lesson
			published int
			createdAt string
			updatedAt string
		)
		if err := rows.Scan(&l.ID, &l.Title, &l.Summary, &l.Passage, &l.Position, &published,
			&createdAt, &updatedAt, &l.QuestionCount); err != nil {
			return nil, fmt.Errorf("store: scan lesson: %w", err)
		}
		l.Published = published == 1
		if c, err := parseTime(createdAt); err == nil {
			l.CreatedAt = c
		}
		if u, err := parseTime(updatedAt); err == nil {
			l.UpdatedAt = u
		}
		lessons = append(lessons, l)
	}
	return lessons, rows.Err()
}

// UpdateLesson updates the content of a lesson.
func (s *Store) UpdateLesson(ctx context.Context, id int64, title, summary, passage string, published bool) error {
	pub := 0
	if published {
		pub = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE lessons SET title = ?, summary = ?, passage = ?, published = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(title), strings.TrimSpace(summary), passage, pub, formatTime(Now()), id)
	if err != nil {
		return fmt.Errorf("store: update lesson: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLesson removes a lesson together with its questions and attempts.
func (s *Store) DeleteLesson(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM lessons WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete lesson: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
