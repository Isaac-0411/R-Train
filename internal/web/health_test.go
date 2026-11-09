package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthProbesArePublicAndDoNotCreateSessions(t *testing.T) {
	srv := newTestServer(t)
	for _, probe := range []struct {
		path        string
		contentType string
		body        string
	}{
		{"/health", "application/json; charset=utf-8", "{\"status\":\"ok\"}\n"},
		{"/healthz", "text/plain; charset=utf-8", "ok"},
	} {
		t.Run(probe.path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				req, err := http.NewRequest(method, srv.URL+probe.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("%s returned %d, want 200", method, resp.StatusCode)
				}
				if got := resp.Header.Get("Content-Type"); got != probe.contentType {
					t.Errorf("Content-Type = %q, want %q", got, probe.contentType)
				}
				wantBody := probe.body
				if method == http.MethodHead {
					wantBody = ""
				}
				if string(body) != wantBody {
					t.Errorf("%s body = %q, want %q", method, body, wantBody)
				}
				if len(resp.Cookies()) != 0 {
					t.Error("probe created a session cookie")
				}
				if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
					t.Error("probe response must not be cached")
				}
			}
		})
	}

	var sessions int
	if err := srv.store.DB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sessions").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Errorf("probes created %d sessions", sessions)
	}
}

func TestHealthReportsUnavailableDatabase(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		path string
		body string
	}{
		{"/health", "{\"status\":\"unavailable\"}\n"},
		{"/healthz", "database unavailable\n"},
	} {
		t.Run(probe.path, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + probe.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", resp.StatusCode)
			}
			if string(body) != probe.body {
				t.Errorf("body = %q, want %q", body, probe.body)
			}
			if len(resp.Cookies()) != 0 {
				t.Error("unavailable probe created a session cookie")
			}
		})
	}
}

func TestHealthRejectsUnsupportedMethods(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/health", "/healthz"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		srv.Config.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s returned %d, want 405", path, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("POST %s Allow = %q, want GET, HEAD", path, got)
		}
	}
}

func TestHealthHonorsRequestCancellation(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("cancelled probe returned %d, want 503", rec.Code)
	}
}
