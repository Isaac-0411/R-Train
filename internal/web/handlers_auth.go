package web

import (
	"errors"
	"net/http"
	"strings"

	"readingtrainer/internal/auth"
	"readingtrainer/internal/middleware"
	"readingtrainer/internal/store"
	"readingtrainer/internal/validate"
)

// handleLoginForm renders the sign in page.
func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "login", map[string]any{
		"Title":    "Sign in",
		"Next":     middleware.CleanPath(r.URL.Query().Get("next")),
		"Username": r.URL.Query().Get("username"),
	})
}

// handleLogin authenticates a user and starts a fresh session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	next := middleware.CleanPath(r.PostFormValue("next"))

	fail := func(message string) {
		s.render(w, r, http.StatusUnauthorized, "login", map[string]any{
			"Title":    "Sign in",
			"Message":  message,
			"Username": username,
			"Next":     next,
		})
	}

	if username == "" || password == "" {
		fail("Please enter your username and password.")
		return
	}

	user, err := s.store.GetUserByUsername(r.Context(), username)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Error("lookup user", "error", err)
		}
		// The same message for an unknown user and a wrong password keeps
		// the response free of account enumeration hints.
		fail("Incorrect username or password.")
		return
	}

	if !auth.VerifyPassword(password, user.PasswordHash) {
		fail("Incorrect username or password.")
		return
	}

	if _, err := s.sessions.Rotate(r.Context(), w, r, user.ID); err != nil {
		s.logger.Error("rotate session", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The session could not be started.")
		return
	}

	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// handleRegisterForm renders the registration page.
func (s *Server) handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "register", map[string]any{
		"Title": "Create account",
	})
}

// handleRegister creates a normal account.
//
// Registration always creates a normal account: administrator accounts can
// only be created by the absolute administrator from the admin area.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	displayName := strings.TrimSpace(r.PostFormValue("display_name"))
	password := r.PostFormValue("password")
	confirmation := r.PostFormValue("password_confirmation")

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

	form := map[string]any{
		"Title":       "Create account",
		"Username":    username,
		"DisplayName": displayName,
		"Errors":      validationErrors,
	}

	if validationErrors.Any() {
		s.render(w, r, http.StatusBadRequest, "register", form)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.logger.Error("hash password", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The account could not be created.")
		return
	}

	user, err := s.store.CreateUser(r.Context(), username, displayName, hash, store.RoleUser, false)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateUsername) {
			validationErrors.Add("username", "That username is already taken.")
			s.render(w, r, http.StatusConflict, "register", form)
			return
		}
		s.logger.Error("create user", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The account could not be created.")
		return
	}

	if _, err := s.sessions.Rotate(r.Context(), w, r, user.ID); err != nil {
		s.logger.Error("rotate session", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The session could not be started.")
		return
	}

	s.setFlash(r, "success", "Welcome, "+user.FullName()+"! Your account is ready.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
