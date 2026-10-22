package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// legacyQuestion is a question in the old format with four fixed options.
type legacyQuestion struct {
	ID        int64
	LessonID  int64
	Prompt    string
	Options   [4]string
	Correct   string
	Position  int
	CreatedAt string
}

// migrateQuestionsToOptions converts a database that still stores the answer
// options in the four fixed columns option_a … option_d into the new format
// with a variable number of options.
//
// A database that already uses the new format is left untouched, so the same
// migration works for fresh and for existing installations.
func migrateQuestionsToOptions(ctx context.Context, tx *sql.Tx) error {
	legacy, err := hasLegacyQuestionColumns(ctx, tx)
	if err != nil {
		return err
	}
	if !legacy {
		return nil
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, lesson_id, prompt, option_a, option_b, option_c, option_d,
		        correct_option, position, created_at
		 FROM questions ORDER BY position ASC, id ASC`)
	if err != nil {
		return fmt.Errorf("store: read legacy questions: %w", err)
	}

	questions := []legacyQuestion{}
	for rows.Next() {
		var q legacyQuestion
		if err := rows.Scan(&q.ID, &q.LessonID, &q.Prompt, &q.Options[0], &q.Options[1],
			&q.Options[2], &q.Options[3], &q.Correct, &q.Position, &q.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("store: scan legacy question: %w", err)
		}
		questions = append(questions, q)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("store: read legacy questions: %w", err)
	}
	rows.Close()

	// The new table is built next to the old one, the old one is dropped
	// afterwards. No foreign key points at the questions table, so dropping
	// it cannot remove any result.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS questions_v2`,
		`CREATE TABLE questions_v2 (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			lesson_id      INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
			prompt         TEXT    NOT NULL,
			options        TEXT    NOT NULL DEFAULT '[]',
			correct_option INTEGER NOT NULL DEFAULT 1,
			position       INTEGER NOT NULL DEFAULT 0,
			created_at     TEXT    NOT NULL
		)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("store: prepare question table: %w", err)
		}
	}

	for _, q := range questions {
		options := Options{}
		for _, text := range q.Options {
			text = strings.TrimSpace(text)
			if text != "" {
				options = append(options, text)
			}
		}
		if len(options) < MinOptions {
			// Keep the question, it just gets two placeholder options.
			options = Options{"(option 1)", "(option 2)"}
		}

		correct := IndexOfLetter(q.Correct)
		if correct < 1 || correct > len(options) {
			correct = 1
		}

		encoded, err := marshalOptions(options)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO questions_v2 (id, lesson_id, prompt, options, correct_option, position, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			q.ID, q.LessonID, q.Prompt, encoded, correct, q.Position, q.CreatedAt); err != nil {
			return fmt.Errorf("store: convert question %d: %w", q.ID, err)
		}
	}

	for _, stmt := range []string{
		`DROP TABLE questions`,
		`ALTER TABLE questions_v2 RENAME TO questions`,
		`CREATE INDEX idx_questions_lesson ON questions(lesson_id, position, id)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("store: replace question table: %w", err)
		}
	}
	return nil
}

// hasLegacyQuestionColumns reports whether the questions table still uses the
// four fixed option columns.
func hasLegacyQuestionColumns(ctx context.Context, tx *sql.Tx) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(questions)`)
	if err != nil {
		return false, fmt.Errorf("store: inspect question table: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, fmt.Errorf("store: inspect question table: %w", err)
		}
		if name == "option_a" {
			return true, nil
		}
	}
	return false, rows.Err()
}
