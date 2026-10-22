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

	lesson, err := st.CreateLesson(ctx, "Lesson 1", "A short summary", "မြန်မာစာပေ", true)
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
		if _, err := st.CreateQuestion(ctx, lesson.ID, "Question?", "a", "b", "c", "d", "b"); err != nil {
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
	if questions[0].Correct != "b" {
		t.Errorf("correct option = %q, want %q", questions[0].Correct, "b")
	}
}

func TestMoveQuestion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lesson, _ := st.CreateLesson(ctx, "Lesson", "", "", true)
	first, _ := st.CreateQuestion(ctx, lesson.ID, "First", "a", "b", "c", "d", "a")
	second, _ := st.CreateQuestion(ctx, lesson.ID, "Second", "a", "b", "c", "d", "a")

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
	lesson, _ := st.CreateLesson(ctx, "Lesson", "", "", true)

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
