package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return st
}

func TestCreateAndReadUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user, err := st.CreateUser(ctx, "alice", "Alice", "hash", RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ID == 0 {
		t.Error("the created user has no id")
	}
	if user.IsRoot {
		t.Error("a normal account must not be the root administrator")
	}

	found, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if found.ID != user.ID {
		t.Errorf("found id %d, want %d", found.ID, user.ID)
	}
	if found.FullName() != "Alice" {
		t.Errorf("FullName() = %q, want %q", found.FullName(), "Alice")
	}
}

func TestUsernameIsUnique(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "alice", "", "hash", RoleUser, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err := st.CreateUser(ctx, "alice", "", "hash", RoleUser, false)
	if !errors.Is(err, ErrDuplicateUsername) {
		t.Fatalf("CreateUser returned %v, want ErrDuplicateUsername", err)
	}
}

func TestOnlyOneRootAdministrator(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "root", "", "hash", RoleAdmin, true); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	hasRoot, err := st.HasRootAdmin(ctx)
	if err != nil {
		t.Fatalf("HasRootAdmin: %v", err)
	}
	if !hasRoot {
		t.Error("HasRootAdmin() = false, want true")
	}

	_, err = st.CreateUser(ctx, "root2", "", "hash", RoleAdmin, true)
	if err == nil {
		t.Error("a second root administrator must not be creatable")
	}
}

func TestDeleteUserCascades(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user, _ := st.CreateUser(ctx, "alice", "", "hash", RoleUser, false)
	if _, err := st.CreateSession(ctx, user.ID, "127.0.0.1", "test", 3600); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := st.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := st.GetUserByID(ctx, user.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByID returned %v, want ErrNotFound", err)
	}

	var sessions int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Errorf("%d sessions survived the deletion of the account", sessions)
	}
}

func TestRootAdministratorCannotBeDeleted(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	root, _ := st.CreateUser(ctx, "root", "", "hash", RoleAdmin, true)
	if err := st.DeleteUser(ctx, root.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUser returned %v, want ErrNotFound", err)
	}
}

func TestLessonAndQuestions(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lesson, err := st.CreateLesson(ctx, "Lesson 1", "A short summary", "မြန်မာစာပေ", "日本語の文章", true)
	if err != nil {
		t.Fatalf("CreateLesson: %v", err)
	}
	if !lesson.Published {
		t.Error("the lesson should be published")
	}

	questions, err := st.ListQuestionsByLesson(ctx, lesson.ID)
	if err != nil {
		t.Fatalf("ListQuestionsByLesson: %v", err)
	}
	if len(questions) != 0 {
		t.Errorf("a new lesson has %d questions, want 0", len(questions))
	}

	for i := 0; i < 3; i++ {
		if _, err := st.CreateQuestion(ctx, lesson.ID, "Question?", []string{"a", "b", "c", "d"}, 2); err != nil {
			t.Fatalf("CreateQuestion: %v", err)
		}
	}

	questions, err = st.ListQuestionsByLesson(ctx, lesson.ID)
	if err != nil {
		t.Fatalf("ListQuestionsByLesson: %v", err)
	}
	if len(questions) != 3 {
		t.Fatalf("got %d questions, want 3", len(questions))
	}
	if questions[0].Position != 1 || questions[2].Position != 3 {
		t.Errorf("questions are not ordered by position: %v", questions)
	}
	if questions[0].CorrectIndex != 2 {
		t.Errorf("correct option = %d, want %d", questions[0].CorrectIndex, 2)
	}
	if questions[0].CorrectLetter() != "B" {
		t.Errorf("correct letter = %q, want %q", questions[0].CorrectLetter(), "B")
	}
	if questions[0].CorrectOption() != "b" {
		t.Errorf("correct option text = %q, want %q", questions[0].CorrectOption(), "b")
	}
}

func TestLessonHasTwoTexts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lesson, err := st.CreateLesson(ctx, "Lesson", "", "မြန်မာစာပေ", "日本語の文章", true)
	if err != nil {
		t.Fatalf("CreateLesson: %v", err)
	}
	if lesson.Passage != "မြန်မာစာပေ" {
		t.Errorf("the lesson text = %q, want %q", lesson.Passage, "မြန်မာစာပေ")
	}
	if lesson.QuizPassage != "日本語の文章" {
		t.Errorf("the question page text = %q, want %q", lesson.QuizPassage, "日本語の文章")
	}

	if err := st.UpdateLesson(ctx, lesson.ID, "Lesson", "", "မြန်မာစာပေ", "新しい文章", false); err != nil {
		t.Fatalf("UpdateLesson: %v", err)
	}

	loaded, err := st.GetLesson(ctx, lesson.ID)
	if err != nil {
		t.Fatalf("GetLesson: %v", err)
	}
	if loaded.QuizPassage != "新しい文章" {
		t.Errorf("the question page text = %q, want %q", loaded.QuizPassage, "新しい文章")
	}
	if loaded.Published {
		t.Error("the lesson should be hidden now")
	}

	lessons, err := st.ListLessons(ctx)
	if err != nil {
		t.Fatalf("ListLessons: %v", err)
	}
	if len(lessons) != 1 {
		t.Fatalf("got %d lessons, want 1", len(lessons))
	}
	if lessons[0].QuizPassage != "新しい文章" {
		t.Errorf("the question page text = %q, want %q", lessons[0].QuizPassage, "新しい文章")
	}
}

func TestQuestionsWithAVariableNumberOfOptions(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lesson, _ := st.CreateLesson(ctx, "Lesson", "", "", "", true)

	question, err := st.CreateQuestion(ctx, lesson.ID, "How many options?",
		[]string{"one", "two", "three", "four", "five", "six"}, 6)
	if err != nil {
		t.Fatalf("CreateQuestion: %v", err)
	}
	if len(question.Options) != 6 {
		t.Fatalf("the question has %d options, want 6", len(question.Options))
	}
	if question.CorrectIndex != 6 {
		t.Errorf("correct option = %d, want %d", question.CorrectIndex, 6)
	}
	if question.CorrectLetter() != "F" {
		t.Errorf("correct letter = %q, want %q", question.CorrectLetter(), "F")
	}
	if question.CorrectOption() != "six" {
		t.Errorf("correct option text = %q, want %q", question.CorrectOption(), "six")
	}

	questions, err := st.ListQuestionsByLesson(ctx, lesson.ID)
	if err != nil {
		t.Fatalf("ListQuestionsByLesson: %v", err)
	}
	if len(questions) != 1 || len(questions[0].Options) != 6 {
		t.Fatalf("the stored question does not have six options: %v", questions)
	}
	if questions[0].Options[5] != "six" {
		t.Errorf("the sixth option = %q, want %q", questions[0].Options[5], "six")
	}

	// Two options are enough, they are a question with two answers.
	short, err := st.CreateQuestion(ctx, lesson.ID, "True or false?",
		[]string{"yes", "no"}, 2)
	if err != nil {
		t.Fatalf("CreateQuestion: %v", err)
	}
	if len(short.Options) != 2 || short.CorrectLetter() != "B" {
		t.Errorf("the short question is wrong: %v", short)
	}
}

func TestLetterHelpers(t *testing.T) {
	if Letter(1) != "A" || Letter(4) != "D" || Letter(8) != "H" {
		t.Error("the letters of the options are wrong")
	}
	if Letter(0) != "" || Letter(9) != "" {
		t.Error("a position outside of the options must not have a letter")
	}
	if IndexOfLetter("a") != 1 || IndexOfLetter("C") != 3 || IndexOfLetter("h") != 8 {
		t.Error("the position of a letter is wrong")
	}
	if IndexOfLetter("") != 0 || IndexOfLetter("i") != 0 || IndexOfLetter("aa") != 0 {
		t.Error("an invalid letter must not have a position")
	}
}

// TestMigrationConvertsLegacyQuestions builds a database in the old format
// with the four fixed option columns and checks that the migration turns it
// into the new format without losing the questions.
func TestMigrationConvertsLegacyQuestions(t *testing.T) {
	ctx := context.Background()

	st, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	legacy := []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (1, '2024-01-01T00:00:00Z')`,
		`CREATE TABLE lessons (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			title      TEXT    NOT NULL,
			summary    TEXT    NOT NULL DEFAULT '',
			passage    TEXT    NOT NULL DEFAULT '',
			position   INTEGER NOT NULL DEFAULT 0,
			published  INTEGER NOT NULL DEFAULT 1,
			created_at TEXT    NOT NULL,
			updated_at TEXT    NOT NULL
		)`,
		`INSERT INTO lessons (id, title, summary, passage, position, published, created_at, updated_at)
		 VALUES (1, 'Lesson 1', '', 'မြန်မာစာပေ', 1, 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
		`CREATE TABLE questions (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			lesson_id      INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
			prompt         TEXT    NOT NULL,
			option_a       TEXT    NOT NULL DEFAULT '',
			option_b       TEXT    NOT NULL DEFAULT '',
			option_c       TEXT    NOT NULL DEFAULT '',
			option_d       TEXT    NOT NULL DEFAULT '',
			correct_option TEXT    NOT NULL CHECK (correct_option IN ('a','b','c','d')),
			position       INTEGER NOT NULL DEFAULT 0,
			created_at     TEXT    NOT NULL
		)`,
		`INSERT INTO questions (id, lesson_id, prompt, option_a, option_b, option_c, option_d, correct_option, position, created_at)
		 VALUES (1, 1, 'Old question', 'first', 'second', 'third', 'fourth', 'c', 1, '2024-01-01T00:00:00Z')`,
		`CREATE TABLE attempt_answers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			attempt_id      INTEGER NOT NULL,
			question_id     INTEGER NOT NULL,
			selected_option TEXT    NOT NULL,
			is_correct      INTEGER NOT NULL
		)`,
		`INSERT INTO attempt_answers (attempt_id, question_id, selected_option, is_correct)
		 VALUES (1, 1, 'c', 1)`,
	}
	for _, stmt := range legacy {
		if _, err := st.DB().ExecContext(ctx, stmt); err != nil {
			t.Fatalf("build legacy database: %v\n%s", err, stmt)
		}
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	questions, err := st.ListQuestionsByLesson(ctx, 1)
	if err != nil {
		t.Fatalf("ListQuestionsByLesson: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("got %d questions, want 1", len(questions))
	}

	question := questions[0]
	if question.Prompt != "Old question" {
		t.Errorf("prompt = %q, want %q", question.Prompt, "Old question")
	}
	if len(question.Options) != 4 {
		t.Fatalf("the question has %d options, want 4", len(question.Options))
	}
	for i, want := range []string{"first", "second", "third", "fourth"} {
		if question.Options[i] != want {
			t.Errorf("option %d = %q, want %q", i+1, question.Options[i], want)
		}
	}
	if question.CorrectIndex != 3 {
		t.Errorf("correct option = %d, want 3", question.CorrectIndex)
	}
	if question.CorrectLetter() != "C" {
		t.Errorf("correct letter = %q, want %q", question.CorrectLetter(), "C")
	}

	// The old result is still there and shows the right texts.
	var selected string
	if err := st.DB().QueryRowContext(ctx,
		`SELECT selected_option FROM attempt_answers WHERE question_id = 1`).Scan(&selected); err != nil {
		t.Fatalf("read attempt answer: %v", err)
	}
	if selected != "c" {
		t.Errorf("the stored answer = %q, want %q", selected, "c")
	}

	// The old lesson got the new column for the text of the question page.
	lesson, err := st.GetLesson(ctx, 1)
	if err != nil {
		t.Fatalf("GetLesson: %v", err)
	}
	if lesson.QuizPassage != "" {
		t.Errorf("the question page text = %q, want it to be empty", lesson.QuizPassage)
	}
}

func TestMoveQuestion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lesson, _ := st.CreateLesson(ctx, "Lesson", "", "", "", true)
	first, _ := st.CreateQuestion(ctx, lesson.ID, "First", []string{"a", "b", "c", "d"}, 1)
	second, _ := st.CreateQuestion(ctx, lesson.ID, "Second", []string{"a", "b", "c", "d"}, 1)

	if err := st.MoveQuestion(ctx, second.ID, -1); err != nil {
		t.Fatalf("MoveQuestion: %v", err)
	}

	questions, _ := st.ListQuestionsByLesson(ctx, lesson.ID)
	if questions[0].ID != second.ID {
		t.Errorf("the second question was not moved up: %v", questions)
	}

	// Moving the first question up is a no-op and must not fail.
	if err := st.MoveQuestion(ctx, questions[0].ID, -1); err != nil {
		t.Errorf("MoveQuestion at the top returned %v, want nil", err)
	}
	_ = first
}

func TestCreateAttemptAndStats(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user, _ := st.CreateUser(ctx, "alice", "", "hash", RoleUser, false)
	lesson, _ := st.CreateLesson(ctx, "Lesson", "", "", "", true)

	answers := []AttemptAnswer{
		{QuestionID: 1, SelectedOption: "a", IsCorrect: true},
		{QuestionID: 2, SelectedOption: "b", IsCorrect: false},
		{QuestionID: 3, SelectedOption: "c", IsCorrect: true},
		{QuestionID: 4, SelectedOption: "d", IsCorrect: true},
	}

	attempt, err := st.CreateAttempt(ctx, user.ID, lesson.ID, 3, 4, answers)
	if err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}
	if attempt.Score != 3 || attempt.Total != 4 {
		t.Errorf("attempt = %d/%d, want 3/4", attempt.Score, attempt.Total)
	}
	if attempt.Percentage() != 75 {
		t.Errorf("Percentage() = %d, want 75", attempt.Percentage())
	}
	if len(attempt.Answers) != 4 {
		t.Errorf("the attempt has %d answers, want 4", len(attempt.Answers))
	}

	stats, err := st.StatsForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("StatsForUser: %v", err)
	}
	if stats.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", stats.Attempts)
	}
	if stats.BestScore != 3 {
		t.Errorf("BestScore = %d, want 3", stats.BestScore)
	}

	// The learner may only see their own results.
	attempts, _ := st.ListAttemptsByUser(ctx, user.ID)
	if len(attempts) != 1 {
		t.Fatalf("ListAttemptsByUser returned %d attempts, want 1", len(attempts))
	}

	if err := st.DeleteAttempt(ctx, attempt.ID); err != nil {
		t.Fatalf("DeleteAttempt: %v", err)
	}
	if _, err := st.GetAttempt(ctx, attempt.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAttempt returned %v, want ErrNotFound", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	session, err := st.CreateSession(ctx, 0, "127.0.0.1", "agent", 3600)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.CSRFToken == "" {
		t.Error("the session has no CSRF token")
	}
	if session.HasUser {
		t.Error("a guest session must not have a user")
	}

	found, err := st.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if found.CSRFToken != session.CSRFToken {
		t.Error("the CSRF token changed between requests")
	}

	kind, message := st.TakeFlash(ctx, session.ID)
	if message != "" {
		t.Errorf("TakeFlash returned %q, want an empty message", message)
	}

	if err := st.SetFlash(ctx, session.ID, "success", "hello"); err != nil {
		t.Fatalf("SetFlash: %v", err)
	}
	kind, message = st.TakeFlash(ctx, session.ID)
	if message != "hello" || kind != "success" {
		t.Errorf("TakeFlash returned (%q, %q), want (success, hello)", kind, message)
	}
	if _, message := st.TakeFlash(ctx, session.ID); message != "" {
		t.Error("the flash message should only be shown once")
	}

	if err := st.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := st.GetSession(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession returned %v, want ErrNotFound", err)
	}
}

func TestRotateSessionKeepsTheUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user, _ := st.CreateUser(ctx, "alice", "", "hash", RoleUser, false)
	old, _ := st.CreateSession(ctx, 0, "127.0.0.1", "agent", 3600)

	rotated, err := st.RotateSession(ctx, old.ID, user.ID, "127.0.0.1", "agent", 3600)
	if err != nil {
		t.Fatalf("RotateSession: %v", err)
	}
	if rotated.ID == old.ID {
		t.Error("the session id must change on rotation")
	}
	if !rotated.HasUser || rotated.UserID != user.ID {
		t.Error("the rotated session is not linked to the user")
	}
	if _, err := st.GetSession(ctx, old.ID); !errors.Is(err, ErrNotFound) {
		t.Error("the old session still exists after the rotation")
	}
}
