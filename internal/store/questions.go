package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// MinOptions is the smallest number of answer options a question may have.
	MinOptions = 2
	// MaxOptions is the largest number of answer options a question may have.
	MaxOptions = 8
)

// Options is the list of answer options of a question.
//
// The administrator decides how many options a question has. The correct
// answer is stored as its one based position inside the list.
type Options []string

// Letter returns the letter of the option at the given one based position.
//
// The first option is "A", the second "B" and so on. An empty string is
// returned for a position outside of the list.
func Letter(position int) string {
	if position < 1 || position > MaxOptions {
		return ""
	}
	return string(rune('A' + position - 1))
}

// IndexOfLetter returns the one based position of an option letter.
//
// It returns 0 when the letter does not belong to a valid option.
func IndexOfLetter(letter string) int {
	trimmed := strings.ToUpper(strings.TrimSpace(letter))
	if len(trimmed) != 1 {
		return 0
	}
	position := int(trimmed[0]-'A') + 1
	if position < 1 || position > MaxOptions {
		return 0
	}
	return position
}

// Question is a multiple choice question belonging to a lesson.
//
// The number of options is chosen by the administrator, it is not fixed.
type Question struct {
	ID       int64
	LessonID int64
	Prompt   string
	// Options are the answer choices in display order.
	Options Options
	// CorrectIndex is the one based position of the correct option.
	CorrectIndex int
	Position     int
	CreatedAt    time.Time
}

// CorrectOption returns the text of the correct option.
func (q Question) CorrectOption() string {
	if q.CorrectIndex < 1 || q.CorrectIndex > len(q.Options) {
		return ""
	}
	return q.Options[q.CorrectIndex-1]
}

// CorrectLetter returns the letter of the correct option.
func (q Question) CorrectLetter() string { return Letter(q.CorrectIndex) }

const questionColumns = `id, lesson_id, prompt, options, correct_option, position, created_at`

func scanQuestion(row interface{ Scan(...any) error }) (*Question, error) {
	var (
		q         Question
		raw       string
		createdAt string
	)
	if err := row.Scan(&q.ID, &q.LessonID, &q.Prompt, &raw, &q.CorrectIndex, &q.Position, &createdAt); err != nil {
		return nil, err
	}
	options, err := unmarshalOptions(raw)
	if err != nil {
		return nil, err
	}
	q.Options = options
	if c, err := parseTime(createdAt); err == nil {
		q.CreatedAt = c
	}
	return &q, nil
}

// CreateQuestion inserts a question and appends it to the end of the lesson.
//
// The options are stored in the given order, correctIndex is the one based
// position of the correct option.
func (s *Store) CreateQuestion(ctx context.Context, lessonID int64, prompt string, options []string, correctIndex int) (*Question, error) {
	var position int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), 0) + 1 FROM questions WHERE lesson_id = ?`, lessonID).Scan(&position); err != nil {
		return nil, fmt.Errorf("store: next question position: %w", err)
	}

	encoded, err := marshalOptions(options)
	if err != nil {
		return nil, err
	}

	var id int64
	id, err = s.execInsert(ctx,
		`INSERT INTO questions (lesson_id, prompt, options, correct_option, position, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		lessonID, strings.TrimSpace(prompt), encoded, correctIndex, position, formatTime(Now()))
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
func (s *Store) UpdateQuestion(ctx context.Context, id int64, prompt string, options []string, correctIndex int) error {
	encoded, err := marshalOptions(options)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE questions SET prompt = ?, options = ?, correct_option = ? WHERE id = ?`,
		strings.TrimSpace(prompt), encoded, correctIndex, id)
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

// marshalOptions encodes the options of a question as a JSON array.
func marshalOptions(options []string) (string, error) {
	if options == nil {
		options = []string{}
	}
	data, err := json.Marshal(options)
	if err != nil {
		return "", fmt.Errorf("store: encode options: %w", err)
	}
	return string(data), nil
}

// unmarshalOptions decodes the JSON array of the options of a question.
func unmarshalOptions(raw string) (Options, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Options{}, nil
	}
	var options Options
	if err := json.Unmarshal([]byte(trimmed), &options); err != nil {
		return nil, fmt.Errorf("store: decode options: %w", err)
	}
	return options, nil
}
