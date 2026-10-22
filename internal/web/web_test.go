package web

import (
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

	// The administrator creates a lesson.
	status, _ := admin.post("/admin/lessons", url.Values{
		"csrf_token": {admin.token("/admin/lessons/new")},
		"title":      {"Lesson 1"},
		"summary":    {"The first lesson"},
		"passage":    {"မြန်မာစာပေ ဖတ်ပါ။"},
		"published":  {"on"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("creating a lesson returned %d, want 303", status)
	}

	// And twelve questions, which makes three boxes of five.
	for i := 1; i <= 12; i++ {
		status, _ = admin.post("/admin/lessons/1/questions", url.Values{
			"csrf_token":     {admin.token("/admin/lessons/1/edit")},
			"prompt":         {"Question " + string(rune('A'+i))},
			"option_a":       {"wrong"},
			"option_b":       {"right"},
			"option_c":       {"wrong"},
			"option_d":       {"wrong"},
			"correct_option": {"b"},
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

	status, body = learner.get("/lessons/1/quiz")
	if status != http.StatusOK {
		t.Fatalf("GET /lessons/1/quiz returned %d, want 200", status)
	}
	if got := strings.Count(body, `class="qbox`); got != 3 {
		t.Errorf("the quiz shows %d boxes, want 3 (12 questions in boxes of 5)", got)
	}
	if !strings.Contains(body, `class="passage"`) {
		t.Error("the text must stay visible next to the questions")
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
		"option_a":       {"a"},
		"option_b":       {"b"},
		"option_c":       {"c"},
		"option_d":       {"d"},
		"correct_option": {"a"},
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
		"option_a":       {"a"},
		"option_b":       {"b"},
		"option_c":       {"c"},
		"option_d":       {"d"},
		"correct_option": {"a"},
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
		"option_a":       {"a"},
		"option_b":       {"b"},
		"option_c":       {"c"},
		"option_d":       {"d"},
		"correct_option": {"a"},
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
