// Package middleware provides the HTTP middleware chain of the application.
//
// The chain is, in order:
//
//	Recoverer      - turns a panic into a 500 page instead of a dropped connection
//	Logger         - structured request logging
//	SecurityHeaders - sensible default headers on every response
//	Sessions.Load  - resolves the session cookie into a session and a user
//	CSRF           - verifies the CSRF token of every state changing request
//	RequireAuth    - only lets signed in users through
//	RequireAdmin   - only lets administrators through
//	RequireRoot    - only lets the absolute (first) administrator through
package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"readingtrainer/internal/store"
)

// Middleware is the standard decorator signature.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares to a handler. The first middleware is the
// outermost one.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// ContextKey is the type used for request scoped values.
type contextKey string

const (
	sessionContextKey contextKey = "rt.session"
	userContextKey    contextKey = "rt.user"
)

// SessionFrom returns the session attached to the request, if any.
func SessionFrom(ctx context.Context) *store.Session {
	sess, _ := ctx.Value(sessionContextKey).(*store.Session)
	return sess
}

// UserFrom returns the signed in user attached to the request, if any.
func UserFrom(ctx context.Context) *store.User {
	user, _ := ctx.Value(userContextKey).(*store.User)
	return user
}

// WithSession stores a session in the context.
func WithSession(ctx context.Context, session *store.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// WithUser stores a user in the context.
func WithUser(ctx context.Context, user *store.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// ErrorRenderer renders an error page for a failed request.
type ErrorRenderer func(w http.ResponseWriter, r *http.Request, status int, message string)

// PlainErrorRenderer is the fallback renderer used when none is supplied.
func PlainErrorRenderer(w http.ResponseWriter, r *http.Request, status int, message string) {
	http.Error(w, message, status)
}

// Recoverer catches panics, logs them and renders a 500 error page.
func Recoverer(logger *slog.Logger, render ErrorRenderer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic while handling request",
						"method", r.Method,
						"path", r.URL.Path,
						"panic", fmt.Sprint(rec),
					)
					render(w, r, http.StatusInternalServerError, "Something went wrong on our side. Please try again.")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder captures the status code and the size of the response.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
		s.ResponseWriter.WriteHeader(code)
	}
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Logger writes one structured log line per request.
func Logger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			level := slog.LevelInfo
			switch {
			case rec.status >= 500:
				level = slog.LevelError
			case rec.status >= 400:
				level = slog.LevelWarn
			}
			logger.Log(r.Context(), level, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration", time.Since(start).String(),
				"ip", ClientIP(r),
			)
		})
	}
}

// SecurityHeaders sets conservative default headers on every response.
func SecurityHeaders(devMode bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-DNS-Prefetch-Control", "off")
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			if devMode {
				// Vite style live reload is not used, but the CSP is relaxed
				// in development so that browser tooling keeps working.
				h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'")
			} else {
				h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'self'")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP extracts the client address, honouring the usual proxy headers.
func ClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
