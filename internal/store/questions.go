package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Question is a multiple choice question belonging to a lesson.
//
// The options are always stored as four fixed columns (option_a .. option_d)
// so that the quiz UI stays simple and consistent.
type Question struct {
	ID         int64
	LessonID   int64
	Prompt     string
	OptionA    string
	OptionB    string
	OptionC    string
	OptionD    string
	Correct    string // "a", "b", "c" or "d"
	Position   int
	CreatedAt  time.Time
	LessonName string
}

// Options returns the four answer options in display order.
func (q Question) Options() [4]string {
	return [4]string{q.OptionA, q.OptionB, q.OptionC, q.OptionD}
}

// OptionText returns the text of an option key.
func (q Question) OptionText(key string) string {
	switch strings.ToLower(key) {
	case "a":
		return q.OptionA
	case "b":
		return q.OptionB
	case "c":
		return q.OptionC
	case "d":
		return q.OptionD
	default:
		return ""
	}
}

const questionColumns = `id, lesson_id, prompt, option_a, option_b, option_c, option_d, correct_option, position, created_at`

func scanQuestion(row interface{ Scan(...any) error }) (*Question, error) {
	var (
		q         Question
		createdAt string
	)
	if err := row.Scan(&q.ID, &q.LessonID, &q.Prompt, &q.OptionA, &q.OptionB, &q.OptionC, &q.OptionD,
		&q.Correct, &q.Position, &createdAt); err != nil {
		return nil, err
	}
	if c, err := parseTime(createdAt); err == nil {
		q.CreatedAt = c
	}
	return &q, nil
}

// CreateQuestion inserts a question and appends it to the end of the lesson.
func (s *Store) CreateQuestion(ctx context.Context, lessonID int64, prompt, a, b, c, d, correct string) (*Question, error) {
	var position int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), 0) + 1 FROM questions WHERE lesson_id = ?`, lessonID).Scan(&position); err != nil {
		return nil, fmt.Errorf("store: next question position: %w", err)
	}

	var id int64
	id, err := s.execInsert(ctx,
		`INSERT INTO questions (lesson_id, prompt, option_a, option_b, option_c, option_d, correct_option, position, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lessonID, strings.TrimSpace(prompt), strings.TrimSpace(a), strings.TrimSpace(b),
		strings.TrimSpace(c), strings.TrimSpace(d), strings.ToLower(correct), position, formatTime(Now()))
	if err != nil {
		return nil, fmt.Errorf("store: insert question: %w", err)
	}
	return s.GetQuestion(ctx, id)
}

// GetQuestion loads a question by its identifier.
func (s *Store) GetQuestion(ctx context.Context, id int64) (*Question, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM questions WHERE id = ?`, id)
	q, err := scanQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get question: %w", err)
	}
	return q, nil
}

// ListQuestionsByLesson returns all questions of a lesson in display order.
func (s *Store) ListQuestionsByLesson(ctx context.Context, lessonID int64) ([]Question, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+questionColumns+` FROM questions WHERE lesson_id = ? ORDER BY position ASC, id ASC`, lessonID)
	if err != nil {
		return nil, fmt.Errorf("store: list questions: %w", err)
	}
	defer rows.Close()

	questions := []Question{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan question: %w", err)
		}
		questions = append(questions, *q)
	}
	return questions, rows.Err()
}

// UpdateQuestion replaces the content of a question.
func (s *Store) UpdateQuestion(ctx context.Context, id int64, prompt, a, b, c, d, correct string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE questions SET prompt = ?, option_a = ?, option_b = ?, option_c = ?, option_d = ?, correct_option = ? WHERE id = ?`,
		strings.TrimSpace(prompt), strings.TrimSpace(a), strings.TrimSpace(b), strings.TrimSpace(c),
		strings.TrimSpace(d), strings.ToLower(correct), id)
	if err != nil {
		return fmt.Errorf("store: update question: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteQuestion removes a question.
func (s *Store) DeleteQuestion(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM questions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete question: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveQuestion shifts a question up or down inside its lesson.
// The new position is one based; 0 means "no change".
func (s *Store) MoveQuestion(ctx context.Context, id int64, direction int) error {
	if direction != -1 && direction != 1 {
		return fmt.Errorf("store: invalid direction %d", direction)
	}
	q, err := s.GetQuestion(ctx, id)
	if err != nil {
		return err
	}
	questions, err := s.ListQuestionsByLesson(ctx, q.LessonID)
	if err != nil {
		return err
	}

	index := -1
	for i, item := range questions {
		if item.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrNotFound
	}
	target := index + direction
	if target < 0 || target >= len(questions) {
		return nil
	}

	other := questions[target]
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin move: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE questions SET position = ? WHERE id = ?`, other.Position, q.ID); err != nil {
		return fmt.Errorf("store: move question: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE questions SET position = ? WHERE id = ?`, q.Position, other.ID); err != nil {
		return fmt.Errorf("store: move question: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit move: %w", err)
	}
	return nil
}

// CountQuestions returns the number of questions in a lesson.
func (s *Store) CountQuestions(ctx context.Context, lessonID int64) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM questions WHERE lesson_id = ?`, lessonID).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: count questions: %w", err)
	}
	return count, nil
}
