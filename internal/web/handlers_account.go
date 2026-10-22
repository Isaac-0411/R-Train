package web

import (
	"net/http"
	"strings"

	"readingtrainer/internal/auth"
	"readingtrainer/internal/validate"
)

// handleAccount renders the profile page of the signed in user.
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	stats, err := s.store.StatsForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("user stats", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The profile could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "account", map[string]any{
		"Title": "My account",
		"Stats": stats,
	})
}

// handleAccountPassword lets a user change their own password.
func (s *Server) handleAccountPassword(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	password := r.PostFormValue("password")
	confirmation := r.PostFormValue("password_confirmation")
	current := r.PostFormValue("current_password")

	validationErrors := validate.New()
	if msg := validate.Password(password); msg != "" {
		validationErrors.Add("password", msg)
	}
	if msg := validate.PasswordConfirmation(password, confirmation); msg != "" {
		validationErrors.Add("password_confirmation", msg)
	}
	if !auth.VerifyPassword(current, user.PasswordHash) {
		validationErrors.Add("current_password", "That is not your current password.")
	}

	stats, _ := s.store.StatsForUser(r.Context(), user.ID)

	if validationErrors.Any() {
		s.render(w, r, http.StatusBadRequest, "account", map[string]any{
			"Title":  "My account",
			"Stats":  stats,
			"Errors": validationErrors,
		})
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.logger.Error("hash password", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The password could not be changed.")
		return
	}

	if err := s.store.UpdateUserPassword(r.Context(), user.ID, hash); err != nil {
		s.logger.Error("update password", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The password could not be changed.")
		return
	}

	// Every other session of this account is invalidated.
	_ = s.store.DeleteUserSessions(r.Context(), user.ID)
	if _, err := s.sessions.Rotate(r.Context(), w, r, user.ID); err != nil {
		s.logger.Error("rotate session", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The session could not be started.")
		return
	}

	s.setFlash(r, "success", "Your password has been changed.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// handleAccountName lets a user change their display name.
func (s *Server) handleAccountName(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	displayName := strings.TrimSpace(r.PostFormValue("display_name"))
	if msg := validate.DisplayName(displayName); msg != "" {
		s.setFlash(r, "error", msg)
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	if err := s.store.UpdateUserProfile(r.Context(), user.ID, displayName); err != nil {
		s.logger.Error("update profile", "error", err)
		s.setFlash(r, "error", "Your profile could not be saved.")
	} else {
		s.setFlash(r, "success", "Your display name has been saved.")
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// handleAccountDelete lets a user delete their own account.
//
// The account and everything that belongs to it — the sessions and the
// results — are removed. The main administrator is protected and keeps its
// account.
func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if user.IsRoot {
		s.setFlash(r, "error", "The main administrator cannot delete the own account.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	if err := s.store.DeleteUser(r.Context(), user.ID); err != nil {
		s.logger.Error("delete user", "error", err)
		s.setFlash(r, "error", "The account could not be deleted.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	// The session of the account is deleted together with the account, so a
	// fresh guest session carries the message to the login page.
	s.logout(w, r, "Your account has been deleted. You are signed out now.")
}
