package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"readingtrainer/internal/auth"
	"readingtrainer/internal/store"
	"readingtrainer/internal/validate"
)

// handleAdminDashboard shows the overview of the system.
func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.DashboardStats(r.Context())
	if err != nil {
		s.logger.Error("dashboard stats", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The statistics could not be loaded.")
		return
	}

	attempts, err := s.store.ListAttempts(r.Context(), 10)
	if err != nil {
		s.logger.Error("list attempts", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The statistics could not be loaded.")
		return
	}

	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.logger.Error("list users", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The statistics could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "admin_dashboard", map[string]any{
		"Title":          "Administration",
		"Stats":          stats,
		"RecentAttempts": attempts,
		"Users":          users,
	})
}

// handleAdminUsers lists every account.
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.logger.Error("list users", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The accounts could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "admin_users", map[string]any{
		"Title":   "Accounts",
		"Users":   users,
		"Created": r.URL.Query().Get("created"),
	})
}

// handleAdminCreateUser creates an account from the admin area.
//
// Normal accounts can be created by every administrator. Administrator
// accounts can only be created by the absolute (first) administrator.
func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	username := strings.TrimSpace(r.PostFormValue("username"))
	displayName := strings.TrimSpace(r.PostFormValue("display_name"))
	password := r.PostFormValue("password")
	confirmation := r.PostFormValue("password_confirmation")
	role := strings.ToLower(strings.TrimSpace(r.PostFormValue("role")))
	if role == "" {
		role = store.RoleUser
	}

	validationErrors := validate.New()
	if msg := validate.Username(username); msg != "" {
		validationErrors.Add("username", msg)
	}
	if msg := validate.DisplayName(displayName); msg != "" {
		validationErrors.Add("display_name", msg)
	}
	if msg := validate.Password(password); msg != "" {
		validationErrors.Add("password", msg)
	}
	if msg := validate.PasswordConfirmation(password, confirmation); msg != "" {
		validationErrors.Add("password_confirmation", msg)
	}
	if msg := validate.Role(role); msg != "" {
		validationErrors.Add("role", msg)
	}
	if role == store.RoleAdmin && !actor.IsRoot {
		validationErrors.Add("role", "Only the main administrator can create administrator accounts.")
	}

	if validationErrors.Any() {
		users, err := s.store.ListUsers(r.Context())
		if err != nil {
			s.logger.Error("list users", "error", err)
		}
		s.render(w, r, http.StatusBadRequest, "admin_users", map[string]any{
			"Title":       "Accounts",
			"Users":       users,
			"Errors":      validationErrors,
			"Username":    username,
			"DisplayName": displayName,
			"Role":        role,
		})
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.logger.Error("hash password", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The account could not be created.")
		return
	}

	isRoot := false
	if role == store.RoleAdmin {
		// The very first administrator is the absolute one; further
		// administrator accounts are ordinary admins.
		hasRoot, err := s.store.HasRootAdmin(r.Context())
		if err != nil {
			s.logger.Error("check root admin", "error", err)
			s.renderError(w, r, http.StatusInternalServerError, "The account could not be created.")
			return
		}
		isRoot = !hasRoot
	}

	if _, err := s.store.CreateUser(r.Context(), username, displayName, hash, role, isRoot); err != nil {
		if errors.Is(err, store.ErrDuplicateUsername) {
			validationErrors.Add("username", "That username is already taken.")
			users, listErr := s.store.ListUsers(r.Context())
			if listErr != nil {
				s.logger.Error("list users", "error", listErr)
			}
			s.render(w, r, http.StatusConflict, "admin_users", map[string]any{
				"Title":       "Accounts",
				"Users":       users,
				"Errors":      validationErrors,
				"Username":    username,
				"DisplayName": displayName,
				"Role":        role,
			})
			return
		}
		s.logger.Error("create user", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The account could not be created.")
		return
	}

	s.setFlash(r, "success", "Account "+username+" has been created.")
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// handleAdminDeleteUser removes an account.
//
// The absolute administrator can never be deleted. Everybody else may delete
// their own account as well.
func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	target, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.logger.Error("get user", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The account could not be deleted.")
		return
	}

	if target.IsRoot {
		s.setFlash(r, "error", "The main administrator cannot be deleted.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	if err := s.store.DeleteUser(r.Context(), target.ID); err != nil {
		s.logger.Error("delete user", "error", err)
		s.setFlash(r, "error", "The account could not be deleted.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	if target.ID == actor.ID {
		// The administrator deleted their own account, the session ends with it.
		s.logout(w, r, "Your account has been deleted. You are signed out now.")
		return
	}

	s.setFlash(r, "success", "Account "+target.Username+" has been deleted.")
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// handleAdminChangeRole promotes or demotes an account.
//
// Only the absolute administrator can hand out or take away the
// administrator role.
func (s *Server) handleAdminChangeRole(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	if !actor.IsRoot {
		s.setFlash(r, "error", "Only the main administrator can change account types.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	role := strings.ToLower(strings.TrimSpace(r.PostFormValue("role")))
	if validate.Role(role) != "" {
		s.setFlash(r, "error", "Unknown account type.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	target, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		s.setFlash(r, "error", "That account does not exist.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if target.IsRoot {
		s.setFlash(r, "error", "The main administrator always keeps the administrator role.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	if err := s.store.UpdateUserRole(r.Context(), target.ID, role); err != nil {
		s.logger.Error("update role", "error", err)
		s.setFlash(r, "error", "The account type could not be changed.")
	} else {
		s.setFlash(r, "success", "Account "+target.Username+" is now a "+role+" account.")
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// handleAdminResetPassword sets a new password for an account.
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	target, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		s.setFlash(r, "error", "That account does not exist.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	if target.IsRoot {
		s.setFlash(r, "error", "The main administrator cannot be changed from here.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if target.IsAdmin() && !actor.IsRoot {
		s.setFlash(r, "error", "Only the main administrator can reset an administrator password.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	password := r.PostFormValue("password")
	if msg := validate.Password(password); msg != "" {
		s.setFlash(r, "error", msg)
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.logger.Error("hash password", "error", err)
		s.setFlash(r, "error", "The password could not be changed.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if err := s.store.UpdateUserPassword(r.Context(), target.ID, hash); err != nil {
		s.logger.Error("update password", "error", err)
		s.setFlash(r, "error", "The password could not be changed.")
	} else {
		s.setFlash(r, "success", "The password of "+target.Username+" has been changed.")
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// handleAdminLessons lists all lessons.
func (s *Server) handleAdminLessons(w http.ResponseWriter, r *http.Request) {
	lessons, err := s.store.ListLessons(r.Context())
	if err != nil {
		s.logger.Error("list lessons", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lessons could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "admin_lessons", map[string]any{
		"Title":   "Lessons",
		"Lessons": lessons,
	})
}

// handleAdminLessonForm renders the create and the edit form of a lesson.
func (s *Server) handleAdminLessonForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.renderLessonForm(w, r, http.StatusOK, map[string]any{
			"Title":     "New lesson",
			"IsNew":     true,
			"Lesson":    &store.Lesson{Published: true},
			"Questions": []store.Question{},
		}, newQuestionEditor())
		return
	}

	lesson, err := s.store.GetLesson(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.logger.Error("get lesson", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be loaded.")
		return
	}

	questions, err := s.store.ListQuestionsByLesson(r.Context(), lesson.ID)
	if err != nil {
		s.logger.Error("list questions", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be loaded.")
		return
	}

	s.renderLessonForm(w, r, http.StatusOK, map[string]any{
		"Title":     "Edit lesson",
		"IsNew":     false,
		"Lesson":    lesson,
		"Questions": questions,
	}, newQuestionEditor())
}

// handleAdminQuestionForm opens the editor of a single question.
func (s *Server) handleAdminQuestionForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	question, err := s.store.GetQuestion(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.logger.Error("get question", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The question could not be loaded.")
		return
	}

	lesson, err := s.store.GetLesson(r.Context(), question.LessonID)
	if err != nil {
		s.notFound(w, r)
		return
	}

	questions, err := s.store.ListQuestionsByLesson(r.Context(), lesson.ID)
	if err != nil {
		s.logger.Error("list questions", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The question could not be loaded.")
		return
	}

	s.renderLessonForm(w, r, http.StatusOK, map[string]any{
		"Title":        "Edit question",
		"IsNew":        false,
		"Lesson":       lesson,
		"Questions":    questions,
		"EditQuestion": question,
	}, questionEditorFor(question))
}

// handleAdminLessonCreate stores a new lesson.
func (s *Server) handleAdminLessonCreate(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.PostFormValue("title"))
	summary := strings.TrimSpace(r.PostFormValue("summary"))
	passage := r.PostFormValue("passage")
	published := r.PostFormValue("published") == "on"

	validationErrors := validate.New()
	if msg := validate.Title(title); msg != "" {
		validationErrors.Add("title", msg)
	}
	if msg := validate.Passage(passage); msg != "" {
		validationErrors.Add("passage", msg)
	}
	if len([]rune(summary)) > 500 {
		validationErrors.Add("summary", "The description must be at most 500 characters long.")
	}

	if validationErrors.Any() {
		s.renderLessonForm(w, r, http.StatusBadRequest, map[string]any{
			"Title":     "New lesson",
			"IsNew":     true,
			"Errors":    validationErrors,
			"Lesson":    &store.Lesson{Title: title, Summary: summary, Passage: passage, Published: published},
			"Questions": []store.Question{},
		}, newQuestionEditor())
		return
	}

	lesson, err := s.store.CreateLesson(r.Context(), title, summary, passage, published)
	if err != nil {
		s.logger.Error("create lesson", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be created.")
		return
	}

	s.setFlash(r, "success", "Lesson "+lesson.Title+" has been created. Add the questions now.")
	http.Redirect(w, r, fmt.Sprintf("/admin/lessons/%d/edit", lesson.ID), http.StatusSeeOther)
}

// handleAdminLessonUpdate changes the content of a lesson.
func (s *Server) handleAdminLessonUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	lesson, err := s.store.GetLesson(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.logger.Error("get lesson", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be loaded.")
		return
	}

	title := strings.TrimSpace(r.PostFormValue("title"))
	summary := strings.TrimSpace(r.PostFormValue("summary"))
	passage := r.PostFormValue("passage")
	published := r.PostFormValue("published") == "on"

	validationErrors := validate.New()
	if msg := validate.Title(title); msg != "" {
		validationErrors.Add("title", msg)
	}
	if msg := validate.Passage(passage); msg != "" {
		validationErrors.Add("passage", msg)
	}
	if len([]rune(summary)) > 500 {
		validationErrors.Add("summary", "The description must be at most 500 characters long.")
	}

	if validationErrors.Any() {
		questions, listErr := s.store.ListQuestionsByLesson(r.Context(), lesson.ID)
		if listErr != nil {
			s.logger.Error("list questions", "error", listErr)
		}
		s.renderLessonForm(w, r, http.StatusBadRequest, map[string]any{
			"Title":     "Edit lesson",
			"IsNew":     false,
			"Errors":    validationErrors,
			"Questions": questions,
			"Lesson": &store.Lesson{
				ID: lesson.ID, Title: title, Summary: summary, Passage: passage,
				Published: published, CreatedAt: lesson.CreatedAt, UpdatedAt: lesson.UpdatedAt,
			},
		}, newQuestionEditor())
		return
	}

	if err := s.store.UpdateLesson(r.Context(), id, title, summary, passage, published); err != nil {
		s.logger.Error("update lesson", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be saved.")
		return
	}

	s.setFlash(r, "success", "Lesson "+title+" has been saved.")
	http.Redirect(w, r, fmt.Sprintf("/admin/lessons/%d/edit", id), http.StatusSeeOther)
}

// handleAdminLessonDelete removes a lesson with its questions and results.
func (s *Server) handleAdminLessonDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	lesson, err := s.store.GetLesson(r.Context(), id)
	if err != nil {
		s.setFlash(r, "error", "That lesson does not exist.")
		http.Redirect(w, r, "/admin/lessons", http.StatusSeeOther)
		return
	}

	if err := s.store.DeleteLesson(r.Context(), id); err != nil {
		s.logger.Error("delete lesson", "error", err)
		s.setFlash(r, "error", "The lesson could not be deleted.")
	} else {
		s.setFlash(r, "success", "Lesson "+lesson.Title+" has been deleted.")
	}
	http.Redirect(w, r, "/admin/lessons", http.StatusSeeOther)
}

// handleAdminQuestionCreate adds a question to a lesson.
func (s *Server) handleAdminQuestionCreate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	lesson, err := s.store.GetLesson(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.logger.Error("get lesson", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be loaded.")
		return
	}

	editor, validationErrors := readQuestionForm(r)

	if validationErrors.Any() {
		s.renderQuestionForm(w, r, http.StatusBadRequest, lesson, nil, validationErrors, editor)
		return
	}

	if _, err := s.store.CreateQuestion(r.Context(), lesson.ID, editor.Prompt, editor.Options, editor.CorrectIndex); err != nil {
		s.logger.Error("create question", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The question could not be saved.")
		return
	}

	s.setFlash(r, "success", "The question has been added.")
	http.Redirect(w, r, fmt.Sprintf("/admin/lessons/%d/edit#questions", lesson.ID), http.StatusSeeOther)
}

// handleAdminQuestionUpdate changes an existing question.
func (s *Server) handleAdminQuestionUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	question, err := s.store.GetQuestion(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.logger.Error("get question", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The question could not be loaded.")
		return
	}

	lesson, err := s.store.GetLesson(r.Context(), question.LessonID)
	if err != nil {
		s.notFound(w, r)
		return
	}

	editor, validationErrors := readQuestionForm(r)

	if validationErrors.Any() {
		s.renderQuestionForm(w, r, http.StatusBadRequest, lesson, question, validationErrors, editor)
		return
	}

	if err := s.store.UpdateQuestion(r.Context(), question.ID, editor.Prompt, editor.Options, editor.CorrectIndex); err != nil {
		s.logger.Error("update question", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The question could not be saved.")
		return
	}

	s.setFlash(r, "success", "The question has been saved.")
	http.Redirect(w, r, fmt.Sprintf("/admin/lessons/%d/edit#questions", lesson.ID), http.StatusSeeOther)
}

// handleAdminQuestionDelete removes a question.
func (s *Server) handleAdminQuestionDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	question, err := s.store.GetQuestion(r.Context(), id)
	if err != nil {
		s.setFlash(r, "error", "That question does not exist.")
		http.Redirect(w, r, "/admin/lessons", http.StatusSeeOther)
		return
	}

	if err := s.store.DeleteQuestion(r.Context(), id); err != nil {
		s.logger.Error("delete question", "error", err)
		s.setFlash(r, "error", "The question could not be deleted.")
	} else {
		s.setFlash(r, "success", "The question has been deleted.")
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/lessons/%d/edit#questions", question.LessonID), http.StatusSeeOther)
}

// handleAdminQuestionMove moves a question up or down inside its lesson.
func (s *Server) handleAdminQuestionMove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	direction, _ := strconv.Atoi(r.PostFormValue("direction"))
	if direction != -1 && direction != 1 {
		direction = 1
	}

	question, err := s.store.GetQuestion(r.Context(), id)
	if err != nil {
		s.setFlash(r, "error", "That question does not exist.")
		http.Redirect(w, r, "/admin/lessons", http.StatusSeeOther)
		return
	}

	if err := s.store.MoveQuestion(r.Context(), id, direction); err != nil {
		s.logger.Error("move question", "error", err)
		s.setFlash(r, "error", "The question could not be moved.")
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/lessons/%d/edit#questions", question.LessonID), http.StatusSeeOther)
}

// handleAdminAttempts lists every result of the system.
func (s *Server) handleAdminAttempts(w http.ResponseWriter, r *http.Request) {
	lessonFilter := strings.TrimSpace(r.URL.Query().Get("lesson"))

	var (
		attempts []store.Attempt
		err      error
	)
	if lessonFilter != "" {
		if id, convErr := strconv.ParseInt(lessonFilter, 10, 64); convErr == nil {
			attempts, err = s.store.ListAttemptsByLesson(r.Context(), id)
		} else {
			s.renderError(w, r, http.StatusBadRequest, "That lesson filter is not valid.")
			return
		}
	} else {
		attempts, err = s.store.ListAttempts(r.Context(), 500)
	}
	if err != nil {
		s.logger.Error("list attempts", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The results could not be loaded.")
		return
	}

	lessons, err := s.store.ListLessons(r.Context())
	if err != nil {
		s.logger.Error("list lessons", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The results could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "admin_attempts", map[string]any{
		"Title":        "Results",
		"Attempts":     attempts,
		"Lessons":      lessons,
		"LessonFilter": lessonFilter,
	})
}

// handleAdminAttemptDelete removes a stored result.
func (s *Server) handleAdminAttemptDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	if err := s.store.DeleteAttempt(r.Context(), id); err != nil {
		s.logger.Error("delete attempt", "error", err)
		s.setFlash(r, "error", "The result could not be deleted.")
	} else {
		s.setFlash(r, "success", "The result has been deleted.")
	}
	http.Redirect(w, r, "/admin/attempts", http.StatusSeeOther)
}

// renderQuestionForm renders the question area of the lesson editor.
func (s *Server) renderQuestionForm(w http.ResponseWriter, r *http.Request, status int, lesson *store.Lesson,
	question *store.Question, validationErrors validate.Errors, editor questionEditor) {

	questions, err := s.store.ListQuestionsByLesson(r.Context(), lesson.ID)
	if err != nil {
		s.logger.Error("list questions", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The questions could not be loaded.")
		return
	}

	s.renderLessonForm(w, r, status, map[string]any{
		"Title":        "Edit lesson",
		"IsNew":        false,
		"Lesson":       lesson,
		"Questions":    questions,
		"Errors":       validationErrors,
		"EditQuestion": question,
	}, editor)
}
