package store

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestRewritePlaceholders(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "no placeholders",
			query: `SELECT 1`,
			want:  `SELECT 1`,
		},
		{
			name:  "two placeholders",
			query: `SELECT * FROM users WHERE id = ? AND role = ?`,
			want:  `SELECT * FROM users WHERE id = $1 AND role = $2`,
		},
		{
			name:  "question mark in a string literal",
			query: `SELECT * FROM lessons WHERE title = 'What? Really?' AND id = ?`,
			want:  `SELECT * FROM lessons WHERE title = 'What? Really?' AND id = $1`,
		},
		{
			name:  "escaped quote in a string literal",
			query: `SELECT 'it''s ?' AS text, ?`,
			want:  `SELECT 'it''s ?' AS text, $1`,
		},
		{
			name:  "quoted identifier",
			query: `SELECT "strange?column" FROM t WHERE id = ?`,
			want:  `SELECT "strange?column" FROM t WHERE id = $1`,
		},
		{
			name:  "line comment",
			query: "SELECT 1 -- why?\nFROM t WHERE id = ?",
			want:  "SELECT 1 -- why?\nFROM t WHERE id = $1",
		},
		{
			name:  "block comment",
			query: `SELECT 1 /* why? */ FROM t WHERE id = ?`,
			want:  `SELECT 1 /* why? */ FROM t WHERE id = $1`,
		},
		{
			name:  "placeholder in LIMIT",
			query: `SELECT * FROM attempts ORDER BY id LIMIT ?`,
			want:  `SELECT * FROM attempts ORDER BY id LIMIT $1`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := rewritePlaceholders(test.query); got != test.want {
				t.Errorf("rewritePlaceholders(%q) = %q, want %q", test.query, got, test.want)
			}
		})
	}
}

// TestPostgresIntegration exercises the complete store on PostgreSQL.
//
// It is skipped unless TEST_DATABASE_URL points at a disposable database.
// The database name must contain "test" so a production URL is never wiped
// by accident.
func TestPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a disposable PostgreSQL database to run this test")
	}
	if !isPostgresDSN(dsn) {
		t.Fatalf("TEST_DATABASE_URL must be a postgres:// or postgresql:// URL")
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !strings.Contains(strings.ToLower(databaseName), "test") {
		t.Fatalf("refusing to run against database %q: the name must contain \"test\"", databaseName)
	}

	st, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if st.Driver() != "postgres" {
		t.Fatalf("Driver() = %q, want postgres", st.Driver())
	}

	ctx := context.Background()
	for _, table := range []string{
		"attempt_answers", "attempts", "questions", "lessons",
		"sessions", "schema_migrations", "users",
	} {
		if _, err := st.DB().ExecContext(ctx, `DROP TABLE IF EXISTS `+table+` CASCADE`); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Running it a second time must be a no-op.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	user, err := st.CreateUser(ctx, "Alice", "Alice", "hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("the created user has no id")
	}
	if _, err := st.GetUserByUsername(ctx, "ALICE"); err != nil {
		t.Fatalf("GetUserByUsername is not case-insensitive: %v", err)
	}
	if _, err := st.CreateUser(ctx, "alice", "", "hash", RoleUser, false); !errors.Is(err, ErrDuplicateUsername) {
		t.Fatalf("duplicate username returned %v, want ErrDuplicateUsername", err)
	}

	root, err := st.CreateUser(ctx, "root", "Root", "hash", RoleAdmin, true)
	if err != nil {
		t.Fatalf("CreateUser root: %v", err)
	}
	if _, err := st.CreateUser(ctx, "root2", "", "hash", RoleAdmin, true); err == nil {
		t.Fatal("a second root administrator must not be creatable")
	}
	if err := st.DeleteUser(ctx, root.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUser(root) = %v, want ErrNotFound", err)
	}

	lesson, err := st.CreateLesson(ctx, "Lesson 1", "summary", "မြန်မာစာပေ", "日本語の文章", true)
	if err != nil {
		t.Fatalf("CreateLesson: %v", err)
	}
	question, err := st.CreateQuestion(ctx, lesson.ID, "Question?", []string{"a", "b", "c"}, 2)
	if err != nil {
		t.Fatalf("CreateQuestion: %v", err)
	}
	if question.CorrectLetter() != "B" {
		t.Errorf("CorrectLetter() = %q, want B", question.CorrectLetter())
	}

	session, err := st.CreateSession(ctx, user.ID, "127.0.0.1", "test", 3600)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	loaded, err := st.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !loaded.HasUser || loaded.UserID != user.ID {
		t.Errorf("the session belongs to %d, want %d", loaded.UserID, user.ID)
	}

	attempt, err := st.CreateAttempt(ctx, user.ID, lesson.ID, 1, 1, []AttemptAnswer{{
		QuestionID:     question.ID,
		SelectedOption: "B",
		IsCorrect:      true,
	}})
	if err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}
	if attempt.ID == 0 {
		t.Fatal("the created attempt has no id")
	}
	if len(attempt.Answers) != 1 || attempt.Answers[0].CorrectIndex != 2 {
		t.Errorf("the stored answer is wrong: %+v", attempt.Answers)
	}

	list, err := st.ListAttempts(ctx, 10)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(list) != 1 || list[0].LessonTitle != "Lesson 1" {
		t.Errorf("ListAttempts returned %+v", list)
	}

	stats, err := st.DashboardStats(ctx)
	if err != nil {
		t.Fatalf("DashboardStats: %v", err)
	}
	if stats.Users != 2 || stats.Lessons != 1 || stats.Attempts != 1 {
		t.Errorf("DashboardStats = %+v", stats)
	}
}
