// Package web contains the HTTP handlers and the routing of the application.
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"readingtrainer/internal/config"
	"readingtrainer/internal/middleware"
	"readingtrainer/internal/store"
)

// Server wires the store, the session manager and the templates together and
// exposes the HTTP routes.
type Server struct {
	cfg       config.Config
	store     *store.Store
	sessions  *middleware.Sessions
	templates *Templates
	logger    *slog.Logger
	handler   http.Handler
}

// New builds a ready to use server.
func New(cfg config.Config, st *store.Store, logger *slog.Logger) (*Server, error) {
	templates, err := loadTemplates()
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:       cfg,
		store:     st,
		sessions:  middleware.NewSessions(st, cfg.SessionCookieName, cfg.CookieSecure, cfg.SessionTTL),
		templates: templates,
		logger:    logger,
	}
	s.handler = s.routes()
	return s, nil
}

// ServeHTTP makes the server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler { return s.handler }

// routes registers every route of the application.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// --- public pages -----------------------------------------------------
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("GET /register", s.handleRegisterForm)
	mux.HandleFunc("POST /register", s.handleRegister)

	// --- static assets ----------------------------------------------------
	if static, err := StaticFS(); err == nil {
		fileServer := http.FileServer(http.FS(static))
		mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))
	}

	// --- health check -----------------------------------------------------
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// --- pages that require a signed in user -------------------------------
	protected := func(h http.HandlerFunc) http.Handler {
		return middleware.Chain(h, middleware.RequireAuth("/login"))
	}

	// "/{$}" matches the landing page only, every other path falls through
	// to the not found handler below.
	mux.Handle("GET /{$}", protected(s.handleLanding))
	mux.Handle("POST /logout", protected(s.handleLogout))
	mux.Handle("POST /session/touch", protected(s.handleSessionTouch))
	mux.Handle("GET /lessons/{id}", protected(s.handleLessonText))
	mux.Handle("GET /lessons/{id}/quiz", protected(s.handleQuiz))
	mux.Handle("POST /lessons/{id}/submit", protected(s.handleSubmit))
	mux.Handle("GET /results", protected(s.handleAttempts))
	mux.Handle("GET /results/{id}", protected(s.handleAttemptDetail))
	mux.Handle("GET /account", protected(s.handleAccount))
	mux.Handle("POST /account/password", protected(s.handleAccountPassword))
	mux.Handle("POST /account/name", protected(s.handleAccountName))
	mux.Handle("POST /account/delete", protected(s.handleAccountDelete))

	// --- administration ----------------------------------------------------
	admin := func(h http.HandlerFunc) http.Handler {
		return middleware.Chain(h, middleware.RequireAdmin(s.renderError))
	}

	mux.Handle("GET /admin", admin(s.handleAdminDashboard))
	mux.Handle("GET /admin/users", admin(s.handleAdminUsers))
	mux.Handle("POST /admin/users", admin(s.handleAdminCreateUser))
	mux.Handle("POST /admin/users/{id}/delete", admin(s.handleAdminDeleteUser))
	mux.Handle("POST /admin/users/{id}/role", admin(s.handleAdminChangeRole))
	mux.Handle("POST /admin/users/{id}/password", admin(s.handleAdminResetPassword))

	mux.Handle("GET /admin/lessons", admin(s.handleAdminLessons))
	mux.Handle("GET /admin/lessons/new", admin(s.handleAdminLessonForm))
	mux.Handle("POST /admin/lessons", admin(s.handleAdminLessonCreate))
	mux.Handle("GET /admin/lessons/{id}/edit", admin(s.handleAdminLessonForm))
	mux.Handle("POST /admin/lessons/{id}", admin(s.handleAdminLessonUpdate))
	mux.Handle("POST /admin/lessons/{id}/delete", admin(s.handleAdminLessonDelete))

	mux.Handle("POST /admin/lessons/{id}/questions", admin(s.handleAdminQuestionCreate))
	mux.Handle("GET /admin/questions/{id}/edit", admin(s.handleAdminQuestionForm))
	mux.Handle("POST /admin/questions/{id}", admin(s.handleAdminQuestionUpdate))
	mux.Handle("POST /admin/questions/{id}/delete", admin(s.handleAdminQuestionDelete))
	mux.Handle("POST /admin/questions/{id}/move", admin(s.handleAdminQuestionMove))

	mux.Handle("GET /admin/attempts", admin(s.handleAdminAttempts))
	mux.Handle("POST /admin/attempts/{id}/delete", admin(s.handleAdminAttemptDelete))

	// --- catch all ---------------------------------------------------------
	mux.HandleFunc("/", s.handleNotFound)

	// --- middleware chain --------------------------------------------------
	return middleware.Chain(mux,
		middleware.Recoverer(s.logger, s.renderError),
		middleware.Logger(s.logger),
		middleware.SecurityHeaders(s.cfg.DevMode),
		s.sessions.Load,
		s.sessions.CSRF(s.renderError),
		middleware.NoStore,
	)
}

// handleHealth answers readiness probes.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.store.DB().PingContext(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleLogout signs the user out and clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	session := middleware.SessionFrom(r.Context())
	if session != nil {
		if err := s.store.DeleteSession(r.Context(), session.ID); err != nil {
			s.logger.Error("delete session", "error", err)
		}
	}
	s.sessions.ClearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleSessionTouch restarts the idle timeout of the current session.
//
// A signed in page sends this request when the visitor interacts with it, so
// a long form (like a quiz) is not cut off by the timeout while the visitor
// is working. The session middleware has already extended the session by the
// time this handler runs, so there is nothing left to do.
func (s *Server) handleSessionTouch(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// logout ends the session of the visitor and sends them to the login page.
//
// The message is stored in a fresh guest session, because the session of a
// deleted account is gone together with the account.
func (s *Server) logout(w http.ResponseWriter, r *http.Request, message string) {
	session, err := s.sessions.Rotate(r.Context(), w, r, 0)
	if err != nil {
		s.logger.Error("rotate session", "error", err)
		s.sessions.ClearCookie(w)
	} else if message != "" {
		if err := s.store.SetFlash(r.Context(), session.ID, "success", message); err != nil {
			s.logger.Error("store flash", "error", err)
		}
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// loadLesson loads a lesson or renders the 404 page.
func (s *Server) loadLesson(w http.ResponseWriter, r *http.Request) (*store.Lesson, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return nil, false
	}
	lesson, err := s.store.GetLesson(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return nil, false
		}
		s.logger.Error("load lesson", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be loaded.")
		return nil, false
	}
	return lesson, true
}
