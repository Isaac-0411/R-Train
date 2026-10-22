package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"readingtrainer/internal/middleware"
	"readingtrainer/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates holds one parsed template set per page. Every page shares the
// same layout ("base") and defines its own "content" block.
type Templates struct {
	pages map[string]*template.Template
}

// templateFuncs are the helpers available inside the templates.
var templateFuncs = template.FuncMap{
	"formatDate": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Local().Format("2 Jan 2006, 15:04")
	},
	"formatDateShort": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Local().Format("2 Jan 2006")
	},
	"percent": func(score, total int) int {
		if total <= 0 {
			return 0
		}
		return int(float64(score) / float64(total) * 100)
	},
	"add":       func(a, b int) int { return a + b },
	"seq":       func(n int) []int { return make([]int, n) },
	"upper":     strings.ToUpper,
	"hasPrefix": strings.HasPrefix,
	"optionLetter": func(i int) string {
		return store.Letter(i + 1)
	},
	"minOptions": func() int { return store.MinOptions },
	"maxOptions": func() int { return store.MaxOptions },
}

// loadTemplates parses every page template together with the shared layout.
func loadTemplates() (*Templates, error) {
	entries, err := fs.ReadDir(templatesFS, "templates")
	if err != nil {
		return nil, fmt.Errorf("web: read templates: %w", err)
	}

	pages := map[string]*template.Template{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".html") || name == "base.html" {
			continue
		}
		tmpl, err := template.New("base.html").Funcs(templateFuncs).ParseFS(
			templatesFS, "templates/base.html", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("web: parse template %s: %w", name, err)
		}
		pages[strings.TrimSuffix(name, ".html")] = tmpl
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("web: no templates found")
	}
	return &Templates{pages: pages}, nil
}

// StaticFS returns the embedded static file system rooted at "static".
func StaticFS() (fs.FS, error) {
	return fs.Sub(staticFS, "static")
}

// render writes a page. The reserved keys (Title, User, CSRFToken, Flash, …)
// are filled in automatically.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}

	ctx := r.Context()
	session := middleware.SessionFrom(ctx)
	user := middleware.UserFrom(ctx)

	if session != nil {
		data["CSRFToken"] = session.CSRFToken
		kind, message := s.store.TakeFlash(ctx, session.ID)
		if message != "" {
			data["Flash"] = message
			data["FlashKind"] = kind
		}
	}
	data["User"] = user
	data["CurrentPath"] = r.URL.Path
	if _, ok := data["Title"]; !ok {
		data["Title"] = "Reading Trainer"
	}

	tmpl, ok := s.templates.pages[page]
	if !ok {
		s.logger.Error("template not found", "page", page)
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		s.logger.Error("render template", "page", page, "error", err)
	}
}

// renderError renders the shared error page.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.render(w, r, status, "error", map[string]any{
		"Title":   "Error",
		"Message": message,
		"Status":  status,
	})
}

// notFound renders the 404 page.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "The page you were looking for does not exist.")
}

// handleNotFound answers every request that did not match a route.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.notFound(w, r)
}

// setFlash stores a one-shot message shown on the next page.
func (s *Server) setFlash(r *http.Request, kind, message string) {
	session := middleware.SessionFrom(r.Context())
	if session == nil {
		return
	}
	if err := s.store.SetFlash(r.Context(), session.ID, kind, message); err != nil {
		s.logger.Error("store flash", "error", err)
	}
}

// currentUser returns the signed in user of the request.
func currentUser(r *http.Request) *store.User {
	return middleware.UserFrom(r.Context())
}

// pathID reads a positive integer path parameter.
func pathID(r *http.Request, name string) (int64, bool) {
	raw := r.PathValue(name)
	if raw == "" {
		return 0, false
	}
	var value int64
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
		value = value*10 + int64(c-'0')
		if value > 1<<40 {
			return 0, false
		}
	}
	if value <= 0 {
		return 0, false
	}
	return value, true
}
