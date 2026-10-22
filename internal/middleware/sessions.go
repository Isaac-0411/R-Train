package middleware

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"readingtrainer/internal/store"
)

// Sessions resolves the session cookie into a session and, when the visitor
// is signed in, into a user.
//
// Every visitor receives a session, signed in or not. Guest sessions hold the
// CSRF token, which keeps a single protection mechanism for all forms.
type Sessions struct {
	store      *store.Store
	cookieName string
	secure     bool
	ttl        time.Duration
}

// NewSessions creates a session manager.
func NewSessions(st *store.Store, cookieName string, secure bool, ttl time.Duration) *Sessions {
	if cookieName == "" {
		cookieName = "rt_session"
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &Sessions{store: st, cookieName: cookieName, secure: secure, ttl: ttl}
}

// CookieName returns the name of the session cookie.
func (s *Sessions) CookieName() string { return s.cookieName }

// Load is the middleware that resolves the session of a request.
func (s *Sessions) Load(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		session, user := s.resolve(ctx, r, w)

		ctx = WithSession(ctx, session)
		if user != nil {
			ctx = WithUser(ctx, user)
		}
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

// resolve finds the session of a request, creating a guest session when the
// cookie is missing or invalid.
func (s *Sessions) resolve(ctx context.Context, r *http.Request, w http.ResponseWriter) (*store.Session, *store.User) {
	cookie, err := r.Cookie(s.cookieName)
	if err == nil && cookie.Value != "" {
		session, err := s.store.GetSession(ctx, cookie.Value)
		if err == nil {
			if session.ExpiresAt.Before(store.Now()) {
				// The idle timeout has passed and the session is over.
				_ = s.store.DeleteSession(ctx, session.ID)
				guest, _ := s.createGuest(ctx, r, w)
				if guest != nil && session.HasUser {
					// Tell the visitor why they have to sign in again.
					_ = s.store.SetFlash(ctx, guest.ID, "error", "Your session timed out. Please sign in again.")
				}
				return guest, nil
			}

			// Every request restarts the idle clock. The cookie is written
			// again with the new expiry, so the browser never drops a
			// session that the server still considers alive.
			expires, err := s.store.TouchSession(ctx, session.ID, s.ttl)
			if err == nil {
				session.ExpiresAt = expires
			}
			s.SetCookie(w, session)

			if session.HasUser {
				user, err := s.store.GetUserByID(ctx, session.UserID)
				if err == nil {
					return session, user
				}
				// The account was deleted while the session was alive.
				_ = s.store.DeleteSession(ctx, session.ID)
				return s.createGuest(ctx, r, w)
			}
			return session, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			// A database problem should not log the visitor out, but it also
			// must not leak a stale session.
			_ = err
		}
	}
	return s.createGuest(ctx, r, w)
}

// createGuest creates a new anonymous session and stores its cookie.
func (s *Sessions) createGuest(ctx context.Context, r *http.Request, w http.ResponseWriter) (*store.Session, *store.User) {
	session, err := s.store.CreateSession(ctx, 0, ClientIP(r), r.UserAgent(), s.ttl)
	if err != nil {
		// Without a session the request can still be served, it just has no
		// CSRF token. The guard middleware reports the problem clearly.
		return nil, nil
	}
	s.SetCookie(w, session)
	return session, nil
}

// SetCookie writes the session cookie. Its lifetime is the same as the
// lifetime of the session, so the browser and the server always agree on when
// the visitor is signed out.
func (s *Sessions) SetCookie(w http.ResponseWriter, session *store.Session) {
	maxAge := int(time.Until(session.ExpiresAt).Round(time.Second).Seconds())
	if maxAge < 1 {
		// A zero Max-Age would turn the cookie into a session cookie that
		// lives until the browser is closed.
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie removes the session cookie from the browser.
func (s *Sessions) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Rotate replaces the current session with a new one, keeping the user
// signed in. It protects against session fixation after a login.
func (s *Sessions) Rotate(ctx context.Context, w http.ResponseWriter, r *http.Request, userID int64) (*store.Session, error) {
	old := SessionFrom(ctx)
	if old != nil {
		_ = s.store.DeleteSession(ctx, old.ID)
	}
	session, err := s.store.CreateSession(ctx, userID, ClientIP(r), r.UserAgent(), s.ttl)
	if err != nil {
		return nil, err
	}
	s.SetCookie(w, session)
	return session, nil
}

// CSRF rejects state changing requests that do not carry the CSRF token of
// the current session.
func (s *Sessions) CSRF(render ErrorRenderer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
				next.ServeHTTP(w, r)
				return
			}

			session := SessionFrom(r.Context())
			if session == nil {
				render(w, r, http.StatusBadRequest, "Your session expired. Please reload the page and try again.")
				return
			}

			if err := r.ParseForm(); err != nil {
				render(w, r, http.StatusBadRequest, "The form could not be read. Please try again.")
				return
			}

			provided := r.PostFormValue("csrf_token")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
				render(w, r, http.StatusForbidden, "The security token was missing or invalid. Please reload the page and try again.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth blocks anonymous visitors. Signed in users are sent to the
// login page, remembering where they wanted to go.
func RequireAuth(loginPath string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if UserFrom(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			target := loginPath
			if r.Method == http.MethodGet {
				if next := r.URL.RequestURI(); next != "" && next != "/" {
					target += "?next=" + url.QueryEscape(next)
				}
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
		})
	}
}

// RequireAdmin blocks everybody who is not an administrator.
func RequireAdmin(render ErrorRenderer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFrom(r.Context())
			if user == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			if !user.IsAdmin() {
				render(w, r, http.StatusForbidden, "Administrator access is required for this page.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRoot blocks everybody who is not the absolute (first) administrator.
func RequireRoot(render ErrorRenderer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFrom(r.Context())
			if user == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			if !user.IsAdmin() || !user.IsRoot {
				render(w, r, http.StatusForbidden, "Only the main administrator can perform this action.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NoStore prevents browsers and proxies from caching authenticated pages.
func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		next.ServeHTTP(w, r)
	})
}

// CleanPath strips a potentially malicious "next" parameter so that only
// internal paths are used for redirects.
func CleanPath(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return ""
	}
	if strings.Contains(next, "\\") || strings.Contains(next, "\n") || strings.Contains(next, "\r") {
		return ""
	}
	return next
}
