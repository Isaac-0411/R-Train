package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Attempt is a submitted quiz result.
type Attempt struct {
	ID          int64
	UserID      int64
	LessonID    int64
	Score       int
	Total       int
	Passed      bool
	SubmittedAt time.Time
	// Joined display fields.
	Username    string
	DisplayName string
	LessonTitle string
	Answers     []AttemptAnswer
}

// Percentage returns the score as a whole percentage.
func (a Attempt) Percentage() int {
	if a.Total <= 0 {
		return 0
	}
	return int(float64(a.Score) / float64(a.Total) * 100)
}

// AttemptAnswer records the answer given for a single question.
type AttemptAnswer struct {
	ID             int64
	AttemptID      int64
	QuestionID     int64
	SelectedOption string
	IsCorrect      bool
	// Joined display fields.
	Prompt       string
	Options      Options
	CorrectIndex int
}

// SelectedText returns the text of the option the user picked.
func (a AttemptAnswer) SelectedText() string {
	index := IndexOfLetter(a.SelectedOption)
	if index < 1 || index > len(a.Options) {
		return ""
	}
	return a.Options[index-1]
}

// CorrectText returns the text of the correct option.
func (a AttemptAnswer) CorrectText() string {
	if a.CorrectIndex < 1 || a.CorrectIndex > len(a.Options) {
		return ""
	}
	return a.Options[a.CorrectIndex-1]
}

// CorrectLetter returns the letter of the correct option.
func (a AttemptAnswer) CorrectLetter() string { return Letter(a.CorrectIndex) }

// CreateAttempt stores a submission together with the answers of the user.
func (s *Store) CreateAttempt(ctx context.Context, userID, lessonID int64, score, total int, answers []AttemptAnswer) (*Attempt, error) {
	passed := 0
	if total > 0 && float64(score)/float64(total) >= 0.5 {
		passed = 1
	}
	now := Now()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin attempt: %w", err)
	}
	defer tx.Rollback()

	var attemptID int64
	res, err := tx.ExecContext(ctx,
		`INSERT INTO attempts (user_id, lesson_id, score, total, passed, submitted_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, lessonID, score, total, passed, formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("store: insert attempt: %w", err)
	}
	if attemptID, err = res.LastInsertId(); err != nil {
		return nil, fmt.Errorf("store: last insert id: %w", err)
	}

	for _, answer := range answers {
		correct := 0
		if answer.IsCorrect {
			correct = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO attempt_answers (attempt_id, question_id, selected_option, is_correct) VALUES (?, ?, ?, ?)`,
			attemptID, answer.QuestionID, answer.SelectedOption, correct); err != nil {
			return nil, fmt.Errorf("store: insert attempt answer: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit attempt: %w", err)
	}
	return s.GetAttempt(ctx, attemptID)
}

// GetAttempt loads a submission with its answers.
func (s *Store) GetAttempt(ctx context.Context, id int64) (*Attempt, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT a.id, a.user_id, a.lesson_id, a.score, a.total, a.passed, a.submitted_at,
		        COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(l.title, '')
		 FROM attempts a
		 LEFT JOIN users u   ON u.id = a.user_id
		 LEFT JOIN lessons l ON l.id = a.lesson_id
		 WHERE a.id = ?`, id)

	var (
		a           Attempt
		passed      int
		submittedAt string
	)
	err := row.Scan(&a.ID, &a.UserID, &a.LessonID, &a.Score, &a.Total, &passed, &submittedAt,
		&a.Username, &a.DisplayName, &a.LessonTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get attempt: %w", err)
	}
	a.Passed = passed == 1
	if t, err := parseTime(submittedAt); err == nil {
		a.SubmittedAt = t
	}

	answers, err := s.ListAttemptAnswers(ctx, id)
	if err != nil {
		return nil, err
	}
	a.Answers = answers
	return &a, nil
}

// ListAttemptAnswers returns the answers of a submission.
func (s *Store) ListAttemptAnswers(ctx context.Context, attemptID int64) ([]AttemptAnswer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT aa.id, aa.attempt_id, aa.question_id, aa.selected_option, aa.is_correct,
		        COALESCE(q.prompt, ''), COALESCE(q.options, '[]'), COALESCE(q.correct_option, 0)
		 FROM attempt_answers aa
		 LEFT JOIN questions q ON q.id = aa.question_id
		 WHERE aa.attempt_id = ? ORDER BY aa.id ASC`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("store: list attempt answers: %w", err)
	}
	defer rows.Close()

	answers := []AttemptAnswer{}
	for rows.Next() {
		var (
			answer    AttemptAnswer
			isCorrect int
			raw       string
		)
		if err := rows.Scan(&answer.ID, &answer.AttemptID, &answer.QuestionID, &answer.SelectedOption,
			&isCorrect, &answer.Prompt, &raw, &answer.CorrectIndex); err != nil {
			return nil, fmt.Errorf("store: scan attempt answer: %w", err)
		}
		options, err := unmarshalOptions(raw)
		if err != nil {
			return nil, err
		}
		answer.Options = options
		answer.IsCorrect = isCorrect == 1
		answers = append(answers, answer)
	}
	return answers, rows.Err()
}

// ListAttemptsByUser returns the submissions of a single user, newest first.
func (s *Store) ListAttemptsByUser(ctx context.Context, userID int64) ([]Attempt, error) {
	return s.queryAttempts(ctx,
		`SELECT a.id, a.user_id, a.lesson_id, a.score, a.total, a.passed, a.submitted_at,
		        COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(l.title, '')
		 FROM attempts a
		 LEFT JOIN users u   ON u.id = a.user_id
		 LEFT JOIN lessons l ON l.id = a.lesson_id
		 WHERE a.user_id = ?
		 ORDER BY a.submitted_at DESC, a.id DESC`, userID)
}

// ListAttempts returns every submission, newest first.
func (s *Store) ListAttempts(ctx context.Context, limit int) ([]Attempt, error) {
	if limit <= 0 {
		limit = 500
	}
	return s.queryAttempts(ctx,
		`SELECT a.id, a.user_id, a.lesson_id, a.score, a.total, a.passed, a.submitted_at,
		        COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(l.title, '')
		 FROM attempts a
		 LEFT JOIN users u   ON u.id = a.user_id
		 LEFT JOIN lessons l ON l.id = a.lesson_id
		 ORDER BY a.submitted_at DESC, a.id DESC LIMIT ?`, limit)
}

// ListAttemptsByLesson returns the submissions of a single lesson.
func (s *Store) ListAttemptsByLesson(ctx context.Context, lessonID int64) ([]Attempt, error) {
	return s.queryAttempts(ctx,
		`SELECT a.id, a.user_id, a.lesson_id, a.score, a.total, a.passed, a.submitted_at,
		        COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(l.title, '')
		 FROM attempts a
		 LEFT JOIN users u   ON u.id = a.user_id
		 LEFT JOIN lessons l ON l.id = a.lesson_id
		 WHERE a.lesson_id = ?
		 ORDER BY a.submitted_at DESC, a.id DESC`, lessonID)
}

func (s *Store) queryAttempts(ctx context.Context, query string, args ...any) ([]Attempt, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list attempts: %w", err)
	}
	defer rows.Close()

	attempts := []Attempt{}
	for rows.Next() {
		var (
			a           Attempt
			passed      int
			submittedAt string
		)
		if err := rows.Scan(&a.ID, &a.UserID, &a.LessonID, &a.Score, &a.Total, &passed, &submittedAt,
			&a.Username, &a.DisplayName, &a.LessonTitle); err != nil {
			return nil, fmt.Errorf("store: scan attempt: %w", err)
		}
		a.Passed = passed == 1
		if t, err := parseTime(submittedAt); err == nil {
			a.SubmittedAt = t
		}
		attempts = append(attempts, a)
	}
	return attempts, rows.Err()
}

// DeleteAttempt removes a submission and its answers.
func (s *Store) DeleteAttempt(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM attempts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete attempt: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Stats summarises the submissions of the whole system.
type Stats struct {
	Users      int
	Admins     int
	Lessons    int
	Questions  int
	Attempts   int
	AvgScore   float64
	PassRate   float64
	BestScore  int
	TotalScore int
}

// DashboardStats computes the statistics shown on the admin dashboard.
func (s *Store) DashboardStats(ctx context.Context) (Stats, error) {
	var stats Stats

	queries := []struct {
		query string
		dest  *int
	}{
		{`SELECT COUNT(*) FROM users`, &stats.Users},
		{`SELECT COUNT(*) FROM users WHERE role = 'admin'`, &stats.Admins},
		{`SELECT COUNT(*) FROM lessons`, &stats.Lessons},
		{`SELECT COUNT(*) FROM questions`, &stats.Questions},
		{`SELECT COUNT(*) FROM attempts`, &stats.Attempts},
	}
	for _, q := range queries {
		if err := s.db.QueryRowContext(ctx, q.query).Scan(q.dest); err != nil {
			return stats, fmt.Errorf("store: dashboard stats: %w", err)
		}
	}

	var passed, totalScore int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(passed), 0), COALESCE(SUM(score), 0) FROM attempts`).
		Scan(&passed, &totalScore); err != nil {
		return stats, fmt.Errorf("store: dashboard stats: %w", err)
	}
	stats.TotalScore = totalScore
	if stats.Attempts > 0 {
		stats.AvgScore = float64(totalScore) / float64(stats.Attempts)
		stats.PassRate = float64(passed) / float64(stats.Attempts) * 100
	}
	return stats, nil
}

// UserStats summarises the results of a single user.
type UserStats struct {
	Attempts   int
	AvgScore   float64
	BestScore  int
	BestTotal  int
	PassRate   float64
	TotalScore int
}

// StatsForUser computes the result statistics of one account.
func (s *Store) StatsForUser(ctx context.Context, userID int64) (UserStats, error) {
	var (
		stats     UserStats
		passCount int
	)

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(score), 0), COALESCE(SUM(passed), 0),
		        COALESCE(MAX(score), 0), COALESCE(MAX(total), 0)
		 FROM attempts WHERE user_id = ?`, userID).
		Scan(&stats.Attempts, &stats.TotalScore, &passCount, &stats.BestScore, &stats.BestTotal); err != nil {
		return stats, fmt.Errorf("store: user stats: %w", err)
	}
	if stats.Attempts > 0 {
		stats.AvgScore = float64(stats.TotalScore) / float64(stats.Attempts)
		stats.PassRate = float64(passCount) / float64(stats.Attempts) * 100
	}
	return stats, nil
}
