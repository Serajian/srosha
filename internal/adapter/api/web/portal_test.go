package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/api/web"
	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

// The portal is tested over the REAL sign-in use case, with the rows in memory.
// A fake of web.SignIn would only prove the handlers call something -- and what
// matters here is that the rules underneath them reach the page unchanged.

type memUsers struct {
	mu   sync.Mutex
	rows map[string]*user.User
}

func (m *memUsers) Create(_ context.Context, u *user.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[u.Email] = u
	return nil
}

func (m *memUsers) ReadByEmail(_ context.Context, email string) (*user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.rows[email]; ok {
		return u, nil
	}
	return nil, errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
}

func (m *memUsers) ReadByID(_ context.Context, id shared.ID) (*user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.rows {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
}

type memCodes struct {
	mu   sync.Mutex
	rows []*logincode.LoginCode
}

func (m *memCodes) Create(_ context.Context, c *logincode.LoginCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, c)
	return nil
}

func (m *memCodes) ReadNewest(_ context.Context, id shared.ID) (*logincode.LoginCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].UserID == id {
			return m.rows[i], nil
		}
	}
	return nil, errs.NotFoundErr("no sign-in code").WithErr(logincode.ErrNotFound)
}

func (m *memCodes) Spend(_ context.Context, _ *logincode.LoginCode) error { return nil }

func (m *memCodes) CountSince(_ context.Context, id shared.ID, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.rows {
		if c.UserID == id && !c.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

type memSessions struct {
	mu   sync.Mutex
	rows map[shared.ID]*session.Session
}

func (m *memSessions) Create(_ context.Context, s *session.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[s.ID] = s
	return nil
}

func (m *memSessions) Read(_ context.Context, id shared.ID) (*session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.rows[id]; ok {
		return s, nil
	}
	return nil, errs.NotFoundErr("session not found").WithErr(session.ErrNotFound)
}

func (m *memSessions) Touch(_ context.Context, _ *session.Session) error { return nil }

func (m *memSessions) Delete(_ context.Context, id shared.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return errs.NotFoundErr("session not found").WithErr(session.ErrNotFound)
	}
	delete(m.rows, id)
	return nil
}

type memMailer struct {
	mu    sync.Mutex
	codes []string
}

func (m *memMailer) SendCode(_ context.Context, _, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes = append(m.codes, code)
	return nil
}

func (m *memMailer) lastCode(t *testing.T) string {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.codes) == 0 {
		t.Fatal("no code was sent")
	}
	return m.codes[len(m.codes)-1]
}

type testPortal struct {
	*httptest.Server
	mail  *memMailer
	users *memUsers
}

func newTestPortal(t *testing.T) *testPortal {
	t.Helper()

	users := &memUsers{rows: map[string]*user.User{}}
	mail := &memMailer{}

	var n int
	ids := func() shared.ID {
		n++
		return shared.ID("01J8XQ2M4E7N9V3B5C6D7F8" + string(rune('A'+n%26)) + "00")
	}

	signIn := usecase.NewSignIn(
		users, &memCodes{}, &memSessions{rows: map[shared.ID]*session.Session{}},
		mail, ids, func() time.Time { return time.Now().UTC() },
	)

	handler, err := web.NewPortal(web.PortalDeps{
		SignIn:       signIn,
		SecureCookie: false,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testPortal{Server: srv, mail: mail, users: users}
}

// --- moving a browser around ---------------------------------------------

// answer is a finished exchange: the body is already read and closed, so no
// test has a lifetime to manage and none of them can leak a connection.
type answer struct {
	status  int
	headers http.Header
	cookies []*http.Cookie
	body    string
}

// location is where the browser was sent, if anywhere.
func (a answer) location() string { return a.headers.Get("Location") }

// session is the cookie that signs somebody in, or nil.
func (a answer) session() *http.Cookie {
	for _, c := range a.cookies {
		if c.Name == "srosha_portal" && c.Value != "" {
			return c
		}
	}
	return nil
}

// do runs one request and finishes it.
//
// Redirects are not followed: what a redirect answers is half of what these
// tests assert.
func do(t *testing.T, req *http.Request, cookies []*http.Cookie) answer {
	t.Helper()

	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return answer{
		status:  res.StatusCode,
		headers: res.Header,
		cookies: res.Cookies(),
		body:    string(body),
	}
}

func post(
	t *testing.T, p *testPortal, path string, form url.Values, cookies ...*http.Cookie,
) answer {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, p.URL+path, strings.NewReader(form.Encode()),
	)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(t, req, cookies)
}

func get(t *testing.T, p *testPortal, path string, cookies ...*http.Cookie) answer {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, p.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return do(t, req, cookies)
}

// --- what the pages must do ------------------------------------------------

// Whatever the address, the answer is the same. Anything else hands the user
// list to whoever is guessing.
func TestAskingForACodeTellsYouNothing(t *testing.T) {
	p := newTestPortal(t)

	known, err := user.New(shared.ID("01K0ACCT0000000000000000AB"), "known@acme.test",
		user.RoleCustomer, time.Now().UTC())
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	off, err := user.New(shared.ID("01K0ACCT0000000000000000AC"), "off@acme.test",
		user.RoleCustomer, time.Now().UTC())
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	off.IsActive = false
	for _, u := range []*user.User{known, off} {
		if err := p.users.Create(context.Background(), u); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	addresses := []string{"known@acme.test", "brand-new@acme.test", "off@acme.test"}

	var answers []string
	for _, email := range addresses {
		got := post(t, p, "/signin", url.Values{"email": {email}})
		if got.status != http.StatusSeeOther {
			t.Fatalf("%s: status %d, want a redirect to the code page", email, got.status)
		}
		// The address itself is the one thing that may differ, and it is what
		// was typed in.
		one := strings.ReplaceAll(got.location()+"|"+got.body, url.QueryEscape(email), "X")
		answers = append(answers, one)
	}

	for i := 1; i < len(answers); i++ {
		if answers[i] != answers[0] {
			t.Errorf("the answer differs between addresses:\n%q\n%q", answers[i], answers[0])
		}
	}
}

func TestSigningInAndOut(t *testing.T) {
	p := newTestPortal(t)

	post(t, p, "/signin", url.Values{"email": {"a@acme.test"}})
	code := p.mail.lastCode(t)

	in := post(t, p, "/signin/code", url.Values{"email": {"a@acme.test"}, "code": {code}})
	cookie := in.session()
	if cookie == nil {
		t.Fatal("no session cookie was set")
	}
	if !cookie.HttpOnly {
		t.Error("the session cookie is readable from javascript")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Error("the session cookie is not SameSite=Lax")
	}

	home := get(t, p, "/", cookie)
	if !strings.Contains(home.body, "a@acme.test") {
		t.Error("the signed-in page does not show who is signed in")
	}

	post(t, p, "/signout", url.Values{}, cookie)

	after := get(t, p, "/", cookie)
	if after.status != http.StatusSeeOther {
		t.Errorf("still signed in after signing out: %d", after.status)
	}
}

func TestTheHomePageNeedsASession(t *testing.T) {
	p := newTestPortal(t)

	got := get(t, p, "/")
	if got.status != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect to sign in", got.status)
	}
	if got.location() != "/signin" {
		t.Errorf("sent to %q", got.location())
	}
}

// A wrong code says the same thing as a wrong address and a missing one. Saying
// which part failed tells whoever is guessing how close they got.
func TestAWrongCodeSaysNothingUseful(t *testing.T) {
	p := newTestPortal(t)

	post(t, p, "/signin", url.Values{"email": {"a@acme.test"}})

	got := post(t, p, "/signin/code", url.Values{"email": {"a@acme.test"}, "code": {"000000"}})
	if got.session() != nil {
		t.Fatal("a wrong code set a session cookie")
	}

	for _, leak := range []string{"expired", "already been used", "not found"} {
		if strings.Contains(strings.ToLower(got.body), leak) {
			t.Errorf("the page says %q, which tells a guesser how close they got", leak)
		}
	}
}

// The one route that must never be reachable by following a link.
func TestSigningOutIsNotAGet(t *testing.T) {
	p := newTestPortal(t)

	if got := get(t, p, "/signout"); got.status != http.StatusMethodNotAllowed {
		t.Errorf("GET /signout = %d, want it refused", got.status)
	}
}
