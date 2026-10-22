package web

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"readingtrainer/internal/auth"
	"readingtrainer/internal/config"
	"readingtrainer/internal/store"
)

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// testServer spins up the application on a temporary database.
type testServer struct {
	*httptest.Server
	store *store.Store
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	return newTestServerWith(t, func(cfg *config.Config) {})
}

// newTestServerWith starts the application on a temporary database. change may
// adjust the configuration first, e.g. to use a short session timeout.
func newTestServerWith(t *testing.T, change func(cfg *config.Config)) *testServer {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	cfg := config.Config{
		Addr:              ":0",
		DBPath:            "test.db",
		SessionTTL:        config.Load().SessionTTL,
		SessionCookieName: "rt_session",
		LogLevel:          "error",
	}
	change(&cfg)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv, err := New(cfg, st, logger)
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}

	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)

	return &testServer{Server: httpServer, store: st}
}

// client is a browser like user with its own cookie jar.
type client struct {
	t    *testing.T
	base string
	jar  http.CookieJar
	http *http.Client
}

func newClient(t *testing.T, base string) *client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &client{
		t:    t,
		base: base,
		jar:  jar,
		http: &http.Client{
			Jar: jar,
			// Redirects are not followed so that the tests can check the
			// status code of the redirect itself.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// get fetches a page and returns the body.
func (c *client) get(path string) (int, string) {
	c.t.Helper()

	resp, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body of %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// post sends a form and returns the status code and the body.
func (c *client) post(path string, form url.Values) (int, string) {
	c.t.Helper()

	resp, err := c.http.PostForm(c.base+path, form)
	if err != nil {
		c.t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body of %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// request fetches a page and also returns the cookies of the response.
func (c *client) request(path string) (int, string, []*http.Cookie) {
	c.t.Helper()

	resp, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body of %s: %v", path, err)
	}
	return resp.StatusCode, string(body), resp.Cookies()
}

// token reads the CSRF token out of a rendered page.
func (c *client) token(path string) string {
	c.t.Helper()

	status, body := c.get(path)
	if status != http.StatusOK {
		c.t.Fatalf("GET %s returned %d, want 200", path, status)
	}
	match := csrfPattern.FindStringSubmatch(body)
	if match == nil {
		c.t.Fatalf("no CSRF token found on %s", path)
	}
	return match[1]
}

// signIn registers or logs in an account and returns the client.
func (c *client) signIn(username, password string) {
	c.t.Helper()

	status, body := c.post("/login", url.Values{
		"csrf_token": {c.token("/login")},
		"username":   {username},
		"password":   {password},
	})
	if status != http.StatusSeeOther {
		c.t.Fatalf("sign in as %s returned %d (%s)", username, status, body)
	}
}

// register creates a normal account through the public form.
func (c *client) register(username, password string) int {
	c.t.Helper()

	status, body := c.post("/register", url.Values{
		"csrf_token":            {c.token("/register")},
		"username":              {username},
		"display_name":          {strings.ToUpper(username[:1]) + username[1:]},
		"password":              {password},
		"password_confirmation": {password},
	})
	if status != http.StatusSeeOther {
		c.t.Fatalf("register %s returned %d (%s)", username, status, body)
	}
	return status
}

// createRootAdmin inserts the main administrator straight into the database.
func (s *testServer) createRootAdmin(t *testing.T, username, password string) {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := s.store.CreateUser(t.Context(), username, "Root Admin", hash, store.RoleAdmin, true); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
}

func TestAnonymousVisitorsAreRedirectedToTheLogin(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)
	for _, path := range []string{"/", "/results", "/lessons/1", "/admin"} {
		status, _ := client.get(path)
		if status != http.StatusSeeOther {
			t.Errorf("GET %s returned %d, want %d", path, status, http.StatusSeeOther)
		}
	}

	if status, _ := client.get("/login"); status != http.StatusOK {
		t.Errorf("GET /login returned %d, want 200", status)
	}
	if status, _ := client.get("/register"); status != http.StatusOK {
		t.Errorf("GET /register returned %d, want 200", status)
	}
	if status, _ := client.get("/does-not-exist"); status != http.StatusNotFound {
		t.Errorf("GET /does-not-exist returned %d, want 404", status)
	}
}

func TestRegisterCreatesANormalAccount(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)

	client.register("alice", "Password123")

	// The account works and the administration area is closed.
	if status, _ := client.get("/"); status != http.StatusOK {
		t.Errorf("GET / returned %d, want 200", status)
	}
	if status, _ := client.get("/admin/users"); status != http.StatusForbidden {
		t.Errorf("GET /admin/users returned %d, want 403", status)
	}

	// A second registration with the same name is refused.
	other := newClient(t, srv.URL)
	status, _ := other.post("/register", url.Values{
		"csrf_token":            {other.token("/register")},
		"username":              {"alice"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
	})
	if status != http.StatusConflict {
		t.Errorf("the duplicate registration returned %d, want 409", status)
	}
}

// findCookie returns the cookie with the given name, or nil.
func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// onlySessionID returns the id of the session that belongs to a signed in user.
func onlySessionID(t *testing.T, srv *testServer) string {
	t.Helper()

	var id string
	err := srv.store.DB().QueryRowContext(t.Context(),
		`SELECT id FROM sessions WHERE user_id IS NOT NULL ORDER BY created_at DESC LIMIT 1`).Scan(&id)
	if err != nil {
		t.Fatalf("find the session: %v", err)
	}
	return id
}

func TestSessionCookieFollowsTheIdleTimeout(t *testing.T) {
	const ttl = 45 * time.Minute

	srv := newTestServerWith(t, func(cfg *config.Config) { cfg.SessionTTL = ttl })
	client := newClient(t, srv.URL)
	client.register("alice", "Password123")

	// The cookie is not a session cookie: it carries the idle timeout.
	status, _, cookies := client.request("/")
	if status != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200", status)
	}
	cookie := findCookie(cookies, "rt_session")
	if cookie == nil {
		t.Fatal("the response did not send a session cookie")
	}
	if want := int(ttl.Seconds()); cookie.MaxAge < want-5 || cookie.MaxAge > want {
		t.Errorf("the session cookie has Max-Age %d, want about %d", cookie.MaxAge, want)
	}
	if cookie.Expires.IsZero() {
		t.Error("the session cookie has no expiry date")
	}

	// A request restarts the idle clock of the stored session.
	id := onlySessionID(t, srv)
	halfway := store.Now().Add(ttl / 2).Format(time.RFC3339)
	if _, err := srv.store.DB().ExecContext(t.Context(),
		`UPDATE sessions SET expires_at = ? WHERE id = ?`, halfway, id); err != nil {
		t.Fatalf("move the expiry: %v", err)
	}

	if status, _, _ := client.request("/"); status != http.StatusOK {
		t.Fatalf("GET / near the timeout returned %d, want 200", status)
	}

	var stored string
	if err := srv.store.DB().QueryRowContext(t.Context(),
		`SELECT expires_at FROM sessions WHERE id = ?`, id).Scan(&stored); err != nil {
		t.Fatalf("read the expiry: %v", err)
	}
	expires, err := time.Parse(time.RFC3339, stored)
	if err != nil {
		t.Fatalf("parse the expiry %q: %v", stored, err)
	}
	if remaining := time.Until(expires); remaining < ttl-time.Minute {
		t.Errorf("the request did not extend the session, it expires in %v", remaining)
	}
}

func TestExpiredSessionSignsTheUserOut(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)
	client.register("alice", "Password123")

	// Let enough time pass without a request.
	id := onlySessionID(t, srv)
	past := store.Now().Add(-time.Minute).Format(time.RFC3339)
	if _, err := srv.store.DB().ExecContext(t.Context(),
		`UPDATE sessions SET expires_at = ? WHERE id = ?`, past, id); err != nil {
		t.Fatalf("expire the session: %v", err)
	}

	status, _, _ := client.request("/")
	if status != http.StatusSeeOther {
		t.Fatalf("GET / with an expired session returned %d, want %d", status, http.StatusSeeOther)
	}
	if _, err := srv.store.GetSession(t.Context(), id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the expired session still exists: %v", err)
	}

	// The sign in page explains what happened.
	status, body := client.get("/login")
	if status != http.StatusOK {
		t.Fatalf("GET /login returned %d, want 200", status)
	}
	if !strings.Contains(body, "Your session timed out") {
		t.Error("the sign in page does not explain the timeout")
	}
}

func TestSessionTouchKeepsTheUserSignedIn(t *testing.T) {
	const ttl = 45 * time.Minute

	srv := newTestServerWith(t, func(cfg *config.Config) { cfg.SessionTTL = ttl })
	client := newClient(t, srv.URL)
	client.register("alice", "Password123")

	token := client.token("/")
	id := onlySessionID(t, srv)
	soon := store.Now().Add(time.Minute).Format(time.RFC3339)
	if _, err := srv.store.DB().ExecContext(t.Context(),
		`UPDATE sessions SET expires_at = ? WHERE id = ?`, soon, id); err != nil {
		t.Fatalf("move the expiry: %v", err)
	}

	status, _ := client.post("/session/touch", url.Values{"csrf_token": {token}})
	if status != http.StatusNoContent {
		t.Fatalf("POST /session/touch returned %d, want %d", status, http.StatusNoContent)
	}

	var stored string
	if err := srv.store.DB().QueryRowContext(t.Context(),
		`SELECT expires_at FROM sessions WHERE id = ?`, id).Scan(&stored); err != nil {
		t.Fatalf("read the expiry: %v", err)
	}
	expires, err := time.Parse(time.RFC3339, stored)
	if err != nil {
		t.Fatalf("parse the expiry %q: %v", stored, err)
	}
	if remaining := time.Until(expires); remaining < ttl-time.Minute {
		t.Errorf("the keep-alive did not extend the session, it expires in %v", remaining)
	}

	// An anonymous visitor cannot extend a session this way.
	anon := newClient(t, srv.URL)
	status, _ = anon.post("/session/touch", url.Values{"csrf_token": {anon.token("/login")}})
	if status != http.StatusSeeOther {
		t.Errorf("an anonymous keep-alive returned %d, want %d", status, http.StatusSeeOther)
	}
}

func TestRegistrationValidatesTheInput(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		name string
		form url.Values
	}{
		{"short password", url.Values{
			"username": {"bob"}, "password": {"short"}, "password_confirmation": {"short"},
		}},
		{"mismatching confirmation", url.Values{
			"username": {"bob"}, "password": {"Password123"}, "password_confirmation": {"Different1"},
		}},
		{"invalid username", url.Values{
			"username": {"b o b"}, "password": {"Password123"}, "password_confirmation": {"Password123"},
		}},
		{"empty username", url.Values{
			"username": {""}, "password": {"Password123"}, "password_confirmation": {"Password123"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, srv.URL)
			tc.form.Set("csrf_token", c.token("/register"))
			status, _ := c.post("/register", tc.form)
			if status != http.StatusBadRequest {
				t.Errorf("the registration returned %d, want 400", status)
			}
		})
	}
}

func TestLoginRejectsAWrongPassword(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)

	status, _ := client.post("/login", url.Values{
		"csrf_token": {client.token("/login")},
		"username":   {"alice"},
		"password":   {"wrong"},
	})
	if status != http.StatusUnauthorized {
		t.Errorf("the login returned %d, want 401", status)
	}
}

func TestCSRFProtection(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)

	// Without a token the registration is refused.
	status, _ := client.post("/register", url.Values{
		"username":              {"alice"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
	})
	if status != http.StatusForbidden {
		t.Errorf("the registration without a CSRF token returned %d, want 403", status)
	}

	// With a wrong token it is refused as well.
	status, _ = client.post("/register", url.Values{
		"csrf_token":            {"not-the-right-token"},
		"username":              {"alice"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
	})
	if status != http.StatusForbidden {
		t.Errorf("the registration with a wrong CSRF token returned %d, want 403", status)
	}
}

func TestAdminCreatesALessonAndLearnerTakesTheQuiz(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	admin := newClient(t, srv.URL)
	admin.signIn("root", "RootPass123")

	// The administrator creates a lesson with two texts: the Myanmar text
	// of the lesson page and the Japanese text of the question page.
	status, _ := admin.post("/admin/lessons", url.Values{
		"csrf_token":   {admin.token("/admin/lessons/new")},
		"title":        {"Lesson 1"},
		"summary":      {"The first lesson"},
		"passage":      {"မြန်မာစာပေ ဖတ်ပါ။"},
		"quiz_passage": {"日本語の文章です。"},
		"published":    {"on"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("creating a lesson returned %d, want 303", status)
	}

	// And twelve questions, which makes twelve boxes with one question each.
	for i := 1; i <= 12; i++ {
		status, _ = admin.post("/admin/lessons/1/questions", url.Values{
			"csrf_token":     {admin.token("/admin/lessons/1/edit")},
			"prompt":         {"Question " + string(rune('A'+i))},
			"option_count":   {"4"},
			"option_1":       {"wrong"},
			"option_2":       {"right"},
			"option_3":       {"wrong"},
			"option_4":       {"wrong"},
			"correct_option": {"2"},
		})
		if status != http.StatusSeeOther {
			t.Fatalf("adding question %d returned %d, want 303", i, status)
		}
	}

	// The learner sees the lesson and the quiz.
	learner := newClient(t, srv.URL)
	learner.register("alice", "Password123")

	status, body := learner.get("/lessons/1")
	if status != http.StatusOK {
		t.Fatalf("GET /lessons/1 returned %d, want 200", status)
	}
	if !strings.Contains(body, "မြန်မာစာပေ") {
		t.Error("the lesson page does not show the text")
	}
	if strings.Contains(body, "日本語の文章です。") {
		t.Error("the lesson page must show the Myanmar text only")
	}

	status, body = learner.get("/lessons/1/quiz")
	if status != http.StatusOK {
		t.Fatalf("GET /lessons/1/quiz returned %d, want 200", status)
	}
	if got := strings.Count(body, `class="qbox`); got != 12 {
		t.Errorf("the quiz shows %d boxes, want 12 (one question per box)", got)
	}
	if !strings.Contains(body, `class="passage passage-jp"`) {
		t.Error("the text box of the question page is missing")
	}
	if !strings.Contains(body, "日本語の文章です。") {
		t.Error("the question page does not show the Japanese text")
	}
	if strings.Contains(body, "မြန်မာစာပေ ဖတ်ပါ။") {
		t.Error("the question page must show the Japanese text, not the Myanmar text")
	}
	if !strings.Contains(body, "Question 1 of 12") {
		t.Error("the question counter is missing")
	}

	// Twelve questions, nine of them answered correctly.
	form := url.Values{"csrf_token": {learner.token("/lessons/1/quiz")}}
	for i := 1; i <= 12; i++ {
		choice := "a"
		if i <= 9 {
			choice = "b"
		}
		form.Set("q_"+itoa(i), choice)
	}

	status, _ = learner.post("/lessons/1/submit", form)
	if status != http.StatusSeeOther {
		t.Fatalf("the submission returned %d, want 303", status)
	}

	status, body = learner.get("/results/1")
	if status != http.StatusOK {
		t.Fatalf("GET /results/1 returned %d, want 200", status)
	}
	if !strings.Contains(body, "9 / 12") {
		t.Errorf("the result page does not show the score 9 / 12:\n%s", body)
	}
	if !strings.Contains(body, `href="/"`) {
		t.Error("the result page has no home button")
	}

	// The result is stored and visible in the history of the learner.
	status, body = learner.get("/results")
	if status != http.StatusOK {
		t.Fatalf("GET /results returned %d, want 200", status)
	}
	if !strings.Contains(body, "Lesson 1") {
		t.Error("the history does not list the result")
	}
}

func TestQuestionPageFallsBackToTheMyanmarText(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	admin := newClient(t, srv.URL)
	admin.signIn("root", "RootPass123")

	// The lesson editor has a field for the text of the question page.
	status, body := admin.get("/admin/lessons/new")
	if status != http.StatusOK {
		t.Fatalf("GET /admin/lessons/new returned %d, want 200", status)
	}
	if !strings.Contains(body, `name="quiz_passage"`) {
		t.Error("the lesson editor has no field for the text of the question page")
	}

	// A lesson without a Japanese text.
	status, _ = admin.post("/admin/lessons", url.Values{
		"csrf_token": {admin.token("/admin/lessons/new")},
		"title":      {"Lesson 2"},
		"passage":    {"မြန်မာစာပေ ဖတ်ပါ။"},
		"published":  {"on"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("creating a lesson returned %d, want 303", status)
	}
	status, _ = admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"Question?"},
		"option_count":   {"2"},
		"option_1":       {"wrong"},
		"option_2":       {"right"},
		"correct_option": {"2"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("adding a question returned %d, want 303", status)
	}

	learner := newClient(t, srv.URL)
	learner.register("bob", "Password123")

	status, body = learner.get("/lessons/1/quiz")
	if status != http.StatusOK {
		t.Fatalf("GET /lessons/1/quiz returned %d, want 200", status)
	}
	if !strings.Contains(body, "မြန်မာစာပေ ဖတ်ပါ။") {
		t.Error("the question page must show the Myanmar text when there is no Japanese text")
	}
	if !strings.Contains(body, "no Japanese text yet") {
		t.Error("the question page does not tell the learner that the Myanmar text is shown")
	}
}

func TestLoginFormCanRevealThePassword(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)

	status, body := client.get("/login")
	if status != http.StatusOK {
		t.Fatalf("GET /login returned %d, want 200", status)
	}
	if !strings.Contains(body, `id="password"`) {
		t.Error("the password field has no id, so the Show button cannot find it")
	}
	if !strings.Contains(body, `data-toggle-password="password"`) {
		t.Error("the login form has no Show button for the password")
	}

	status, body = client.get("/register")
	if status != http.StatusOK {
		t.Fatalf("GET /register returned %d, want 200", status)
	}
	for _, id := range []string{"password", "password_confirmation"} {
		if !strings.Contains(body, `id="`+id+`"`) {
			t.Errorf("the field %q has no id, so the Show button cannot find it", id)
		}
		if !strings.Contains(body, `data-toggle-password="`+id+`"`) {
			t.Errorf("the field %q has no Show button", id)
		}
	}
}

func TestSubmissionRequiresEveryAnswer(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	admin := newClient(t, srv.URL)
	admin.signIn("root", "RootPass123")
	admin.post("/admin/lessons", url.Values{
		"csrf_token": {admin.token("/admin/lessons/new")},
		"title":      {"Lesson 1"},
		"passage":    {"text"},
		"published":  {"on"},
	})
	admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"Question?"},
		"option_count":   {"4"},
		"option_1":       {"a"},
		"option_2":       {"b"},
		"option_3":       {"c"},
		"option_4":       {"d"},
		"correct_option": {"1"},
	})

	learner := newClient(t, srv.URL)
	learner.register("alice", "Password123")

	status, _ := learner.post("/lessons/1/submit", url.Values{
		"csrf_token": {learner.token("/lessons/1/quiz")},
	})
	if status != http.StatusBadRequest {
		t.Errorf("the incomplete submission returned %d, want 400", status)
	}
}

func TestOnlyTheMainAdministratorCreatesAdministrators(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	root := newClient(t, srv.URL)
	root.signIn("root", "RootPass123")

	// The main administrator creates a second administrator.
	status, _ := root.post("/admin/users", url.Values{
		"csrf_token":            {root.token("/admin/users")},
		"username":              {"admin2"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
		"role":                  {"admin"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("creating an administrator returned %d, want 303", status)
	}

	// The second administrator may not create further administrators.
	second := newClient(t, srv.URL)
	second.signIn("admin2", "Password123")

	status, _ = second.post("/admin/users", url.Values{
		"csrf_token":            {second.token("/admin/users")},
		"username":              {"admin3"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
		"role":                  {"admin"},
	})
	if status != http.StatusBadRequest {
		t.Errorf("the second administrator returned %d, want 400", status)
	}

	// But may create normal accounts.
	status, _ = second.post("/admin/users", url.Values{
		"csrf_token":            {second.token("/admin/users")},
		"username":              {"alice"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
		"role":                  {"user"},
	})
	if status != http.StatusSeeOther {
		t.Errorf("creating a normal account returned %d, want 303", status)
	}
}

func TestTheMainAdministratorIsProtected(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	root := newClient(t, srv.URL)
	root.signIn("root", "RootPass123")

	// Even the main administrator cannot delete itself.
	status, _ := root.post("/admin/users/1/delete", url.Values{
		"csrf_token": {root.token("/admin/users")},
	})
	if status != http.StatusSeeOther {
		t.Errorf("the deletion returned %d, want 303", status)
	}
	if _, err := srv.store.GetUserByID(t.Context(), 1); err != nil {
		t.Errorf("the main administrator was deleted: %v", err)
	}

	// A second administrator cannot delete the main administrator either.
	root.post("/admin/users", url.Values{
		"csrf_token":            {root.token("/admin/users")},
		"username":              {"admin2"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
		"role":                  {"admin"},
	})

	second := newClient(t, srv.URL)
	second.signIn("admin2", "Password123")

	status, _ = second.post("/admin/users/1/delete", url.Values{
		"csrf_token": {second.token("/admin/users")},
	})
	if status != http.StatusSeeOther {
		t.Errorf("the deletion returned %d, want 303", status)
	}
	if _, err := srv.store.GetUserByID(t.Context(), 1); err != nil {
		t.Errorf("the main administrator was deleted: %v", err)
	}
}

func TestAdministratorsUseTheSiteLikeLearners(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	root := newClient(t, srv.URL)
	root.signIn("root", "RootPass123")

	if status, _ := root.get("/"); status != http.StatusOK {
		t.Errorf("GET / returned %d, want 200", status)
	}
	if status, _ := root.get("/results"); status != http.StatusOK {
		t.Errorf("GET /results returned %d, want 200", status)
	}
	if status, _ := root.get("/admin"); status != http.StatusOK {
		t.Errorf("GET /admin returned %d, want 200", status)
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)
	client.register("alice", "Password123")

	if status, _ := client.get("/"); status != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200", status)
	}

	status, _ := client.post("/logout", url.Values{"csrf_token": {client.token("/")}})
	if status != http.StatusSeeOther {
		t.Errorf("the logout returned %d, want 303", status)
	}
	if status, _ := client.get("/"); status != http.StatusSeeOther {
		t.Errorf("GET / after the logout returned %d, want 303", status)
	}
}

func TestResultOfAnotherLearnerIsHidden(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	admin := newClient(t, srv.URL)
	admin.signIn("root", "RootPass123")
	admin.post("/admin/lessons", url.Values{
		"csrf_token": {admin.token("/admin/lessons/new")},
		"title":      {"Lesson 1"},
		"passage":    {"text"},
		"published":  {"on"},
	})
	admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"Question?"},
		"option_count":   {"4"},
		"option_1":       {"a"},
		"option_2":       {"b"},
		"option_3":       {"c"},
		"option_4":       {"d"},
		"correct_option": {"1"},
	})

	alice := newClient(t, srv.URL)
	alice.register("alice", "Password123")
	alice.post("/lessons/1/submit", url.Values{
		"csrf_token": {alice.token("/lessons/1/quiz")},
		"q_1":        {"a"},
	})

	bob := newClient(t, srv.URL)
	bob.register("bob", "Password123")
	if status, _ := bob.get("/results/1"); status != http.StatusForbidden {
		t.Errorf("GET /results/1 returned %d, want 403", status)
	}

	// The administrator may look at every result.
	if status, _ := admin.get("/results/1"); status != http.StatusOK {
		t.Errorf("GET /results/1 as administrator returned %d, want 200", status)
	}
}

func TestAdminCanDeleteResults(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	admin := newClient(t, srv.URL)
	admin.signIn("root", "RootPass123")
	admin.post("/admin/lessons", url.Values{
		"csrf_token": {admin.token("/admin/lessons/new")},
		"title":      {"Lesson 1"},
		"passage":    {"text"},
		"published":  {"on"},
	})
	admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"Question?"},
		"option_count":   {"4"},
		"option_1":       {"a"},
		"option_2":       {"b"},
		"option_3":       {"c"},
		"option_4":       {"d"},
		"correct_option": {"1"},
	})

	learner := newClient(t, srv.URL)
	learner.register("alice", "Password123")
	learner.post("/lessons/1/submit", url.Values{
		"csrf_token": {learner.token("/lessons/1/quiz")},
		"q_1":        {"a"},
	})

	status, _ := admin.post("/admin/attempts/1/delete", url.Values{
		"csrf_token": {admin.token("/admin/attempts")},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("the deletion returned %d, want 303", status)
	}
	if _, err := srv.store.GetAttempt(t.Context(), 1); err == nil {
		t.Error("the result was not deleted")
	}
}

func TestLearnerCannotReachTheAdminArea(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)
	client.register("alice", "Password123")

	for _, path := range []string{"/admin", "/admin/users", "/admin/lessons", "/admin/attempts"} {
		if status, _ := client.get(path); status != http.StatusForbidden {
			t.Errorf("GET %s returned %d, want 403", path, status)
		}
	}
}

func TestALearnerCanDeleteTheirOwnAccount(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv.URL)
	client.register("alice", "Password123")

	status, body := client.get("/account")
	if status != http.StatusOK {
		t.Fatalf("GET /account returned %d, want 200", status)
	}
	if !strings.Contains(body, "/account/delete") {
		t.Error("the account page has no delete button")
	}

	status, _ = client.post("/account/delete", url.Values{
		"csrf_token": {client.token("/account")},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("the deletion returned %d, want 303", status)
	}

	// The account is gone and the session does not work any more.
	if _, err := srv.store.GetUserByUsername(t.Context(), "alice"); err == nil {
		t.Error("the account was not deleted")
	}
	if status, _ := client.get("/"); status != http.StatusSeeOther {
		t.Errorf("GET / after the deletion returned %d, want 303", status)
	}

	// The login page tells the visitor what happened.
	status, body = client.get("/login")
	if status != http.StatusOK {
		t.Fatalf("GET /login returned %d, want 200", status)
	}
	if !strings.Contains(body, "deleted") {
		t.Errorf("the login page does not mention the deletion:\n%s", body)
	}
}

func TestAnAdministratorCanDeleteTheirOwnAccount(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	// A second administrator, created by the main administrator.
	root := newClient(t, srv.URL)
	root.signIn("root", "RootPass123")
	root.post("/admin/users", url.Values{
		"csrf_token":            {root.token("/admin/users")},
		"username":              {"admin2"},
		"password":              {"Password123"},
		"password_confirmation": {"Password123"},
		"role":                  {"admin"},
	})

	second := newClient(t, srv.URL)
	second.signIn("admin2", "Password123")

	status, _ := second.post("/admin/users/2/delete", url.Values{
		"csrf_token": {second.token("/admin/users")},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("the deletion returned %d, want 303", status)
	}
	if _, err := srv.store.GetUserByID(t.Context(), 2); err == nil {
		t.Error("the account was not deleted")
	}
	if status, _ := second.get("/admin/users"); status != http.StatusSeeOther {
		t.Errorf("GET /admin/users after the deletion returned %d, want 303", status)
	}
}

func TestTheMainAdministratorCannotDeleteTheOwnAccount(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	root := newClient(t, srv.URL)
	root.signIn("root", "RootPass123")

	// The account page does not offer the deletion.
	status, body := root.get("/account")
	if status != http.StatusOK {
		t.Fatalf("GET /account returned %d, want 200", status)
	}
	if strings.Contains(body, "/account/delete") {
		t.Error("the main administrator must not be able to delete the own account")
	}

	// And the request itself is refused.
	status, _ = root.post("/account/delete", url.Values{
		"csrf_token": {root.token("/account")},
	})
	if status != http.StatusSeeOther {
		t.Errorf("the deletion returned %d, want 303", status)
	}
	if _, err := srv.store.GetUserByID(t.Context(), 1); err != nil {
		t.Errorf("the main administrator was deleted: %v", err)
	}
}

func TestQuestionsWithAVariableNumberOfOptions(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	admin := newClient(t, srv.URL)
	admin.signIn("root", "RootPass123")

	admin.post("/admin/lessons", url.Values{
		"csrf_token": {admin.token("/admin/lessons/new")},
		"title":      {"Lesson 1"},
		"passage":    {"မြန်မာစာပေ"},
		"published":  {"on"},
	})

	// A question with six options.
	status, _ := admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"How many options does this question have?"},
		"option_count":   {"6"},
		"option_1":       {"one"},
		"option_2":       {"two"},
		"option_3":       {"three"},
		"option_4":       {"four"},
		"option_5":       {"five"},
		"option_6":       {"six"},
		"correct_option": {"6"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("adding the question returned %d, want 303", status)
	}

	// A question with only two options.
	status, _ = admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"True or false?"},
		"option_count":   {"2"},
		"option_1":       {"yes"},
		"option_2":       {"no"},
		"correct_option": {"1"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("adding the question returned %d, want 303", status)
	}

	learner := newClient(t, srv.URL)
	learner.register("alice", "Password123")

	status, body := learner.get("/lessons/1/quiz")
	if status != http.StatusOK {
		t.Fatalf("GET /lessons/1/quiz returned %d, want 200", status)
	}
	if !strings.Contains(body, `value="F"`) {
		t.Error("the quiz does not show the sixth option")
	}
	if !strings.Contains(body, "six") {
		t.Error("the quiz does not show the text of the sixth option")
	}
	// The second question has two options, the sixth letter must not be there.
	if strings.Count(body, `value="F"`) != 1 {
		t.Errorf("the sixth option is shown %d times, want 1", strings.Count(body, `value="F"`))
	}

	// The answers are graded with the position of the option.
	status, _ = learner.post("/lessons/1/submit", url.Values{
		"csrf_token": {learner.token("/lessons/1/quiz")},
		"q_1":        {"f"},
		"q_2":        {"a"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("the submission returned %d, want 303", status)
	}

	status, body = learner.get("/results/1")
	if status != http.StatusOK {
		t.Fatalf("GET /results/1 returned %d, want 200", status)
	}
	if !strings.Contains(body, "2 / 2") {
		t.Errorf("the result page does not show the score 2 / 2:\n%s", body)
	}
}

func TestTheQuestionEditorRefusesBrokenOptionCounts(t *testing.T) {
	srv := newTestServer(t)
	srv.createRootAdmin(t, "root", "RootPass123")

	admin := newClient(t, srv.URL)
	admin.signIn("root", "RootPass123")

	admin.post("/admin/lessons", url.Values{
		"csrf_token": {admin.token("/admin/lessons/new")},
		"title":      {"Lesson 1"},
		"passage":    {"text"},
		"published":  {"on"},
	})

	// Only one option.
	status, _ := admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"Question?"},
		"option_count":   {"1"},
		"option_1":       {"only one"},
		"correct_option": {"1"},
	})
	if status != http.StatusBadRequest {
		t.Errorf("the question with one option returned %d, want 400", status)
	}

	// Too many options.
	status, _ = admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"Question?"},
		"option_count":   {"9"},
		"option_1":       {"a"},
		"option_2":       {"b"},
		"option_3":       {"c"},
		"option_4":       {"d"},
		"option_5":       {"e"},
		"option_6":       {"f"},
		"option_7":       {"g"},
		"option_8":       {"h"},
		"option_9":       {"i"},
		"correct_option": {"1"},
	})
	if status != http.StatusBadRequest {
		t.Errorf("the question with nine options returned %d, want 400", status)
	}

	// An empty option.
	status, _ = admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":     {admin.token("/admin/lessons/1/edit")},
		"prompt":         {"Question?"},
		"option_count":   {"3"},
		"option_1":       {"a"},
		"option_2":       {"  "},
		"option_3":       {"c"},
		"correct_option": {"1"},
	})
	if status != http.StatusBadRequest {
		t.Errorf("the question with an empty option returned %d, want 400", status)
	}

	// No correct option.
	status, _ = admin.post("/admin/lessons/1/questions", url.Values{
		"csrf_token":   {admin.token("/admin/lessons/1/edit")},
		"prompt":       {"Question?"},
		"option_count": {"2"},
		"option_1":     {"a"},
		"option_2":     {"b"},
	})
	if status != http.StatusBadRequest {
		t.Errorf("the question without a correct option returned %d, want 400", status)
	}

	questions, err := srv.store.ListQuestionsByLesson(t.Context(), 1)
	if err != nil {
		t.Fatalf("ListQuestionsByLesson: %v", err)
	}
	if len(questions) != 0 {
		t.Errorf("%d broken questions were saved, want 0", len(questions))
	}
}

// itoa is a tiny helper so the test does not need the strconv import.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}
