package web_test

import (
	"context"
	"fmt"
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
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
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

// memSources is the source repository these tests run over. Ownership is a
// WHERE clause in postgres and a filter here, and the tests care that the rule
// holds rather than where it is written.
type memSources struct {
	mu   sync.Mutex
	rows []*source.Source
}

func (m *memSources) Create(_ context.Context, s *source.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, s)
	return nil
}

func (m *memSources) ReadByID(_ context.Context, id string) (*source.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.rows {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
}

func (m *memSources) ListByOwner(
	_ context.Context, owner shared.ID,
) ([]source.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []source.Source{}
	for _, s := range m.rows {
		if s.OwnerUserID == owner {
			out = append(out, *s)
		}
	}
	return out, nil
}

// memAudit is the gate's log. Nothing asserts on it here -- the use case tests
// do that -- but the gate refuses to run anything it cannot record, so a
// portal test without one would fail on every mutating route.
type memAudit struct {
	mu      sync.Mutex
	entries []usecase.AuditEntry
}

func (m *memAudit) Record(_ context.Context, e usecase.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

// memKeys is the key store these tests run over. It records the hashes so a
// test can prove the key itself was never written.
type memKeys struct {
	mu     sync.Mutex
	keys   []source.Key
	hashes []string
}

func (m *memKeys) Create(_ context.Context, k *source.Key, keyHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = append(m.keys, *k)
	m.hashes = append(m.hashes, keyHash)
	return nil
}

func (m *memKeys) ListBySourceID(_ context.Context, id string) ([]source.Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []source.Key{}
	for _, k := range m.keys {
		if k.SourceID == id {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memKeys) Revoke(_ context.Context, id shared.ID, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.keys {
		if m.keys[i].ID == id {
			m.keys[i].RevokedAt = &now
			return nil
		}
	}
	return errs.NotFoundErr("no such key").WithErr(source.ErrKeyNotFound)
}

// mintKeys hands out a different key each time, so two are told apart.
type mintKeys struct {
	mu sync.Mutex
	n  int
}

func (m *mintKeys) Mint() (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	return fmt.Sprintf("srosha_sk_%03d", m.n), fmt.Sprintf("hash_%03d", m.n), nil
}

// memSenders and memCallbacks stand in for the two use cases this adapter
// declares interfaces for. The rules underneath them -- what a credential
// needs, that a callback must be https -- are tested in their own packages;
// faking them here would only test the fake.
type memSenders struct {
	mu   sync.Mutex
	rows map[string][]credential.Credential
}

func (m *memSenders) Register(
	_ context.Context, sourceID string, reg usecase.CredentialRegistration,
) (*credential.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := credential.Credential{Name: reg.Name, Channel: reg.Channel}
	m.rows[sourceID] = append(m.rows[sourceID], c)
	return &c, nil
}

func (m *memSenders) List(
	_ context.Context, sourceID string,
) ([]credential.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[sourceID], nil
}

type memCallbacks struct {
	mu   sync.Mutex
	rows map[string]*webhook.Webhook
	n    int
}

// Register hands back a secret exactly once, which is the contract the page is
// built on: Get never returns one, because nothing kept it in the clear.
func (m *memCallbacks) Register(
	_ context.Context, sourceID string, reg webhook.Registration,
) (*webhook.Webhook, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	w := webhook.Restore(webhook.Snapshot{
		SourceID: sourceID, CallbackURL: reg.CallbackURL, IsActive: true,
	})
	m.rows[sourceID] = w
	return w, fmt.Sprintf("whsec_%03d", m.n), nil
}

func (m *memCallbacks) Get(_ context.Context, sourceID string) (*webhook.Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.rows[sourceID]
	if !ok {
		return nil, errs.NotFoundErr("no callback").WithErr(webhook.ErrNotFound)
	}
	return w, nil
}

type testPortal struct {
	*httptest.Server
	mail    *memMailer
	users   *memUsers
	sources *memSources
	keys    *memKeys
	audit   *memAudit
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

	now := func() time.Time { return time.Now().UTC() }

	signIn := usecase.NewSignIn(
		users, &memCodes{}, &memSessions{rows: map[shared.ID]*session.Session{}},
		mail, ids, now,
	)

	sources := &memSources{}
	audit := &memAudit{}
	gate := usecase.NewGate(audit, ids, now)

	useSources := usecase.NewSources(sources, gate, ids, now)
	keys := &memKeys{}

	handler, err := web.NewPortal(web.PortalDeps{
		SignIn:       signIn,
		Sources:      useSources,
		Keys:         usecase.NewKeys(keys, useSources, &mintKeys{}, gate, ids, now),
		Senders:      &memSenders{rows: map[string][]credential.Credential{}},
		Callbacks:    &memCallbacks{rows: map[string]*webhook.Webhook{}},
		SecureCookie: false,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testPortal{
		Server: srv, mail: mail, users: users, sources: sources, keys: keys, audit: audit,
	}
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

// --- the source pages ------------------------------------------------------

// signedIn runs the whole sign-in flow and hands back the cookie, so a test
// about sources is not also a test about signing in.
func signedIn(t *testing.T, p *testPortal, email string) *http.Cookie {
	t.Helper()

	post(t, p, "/signin", url.Values{"email": {email}})
	in := post(t, p, "/signin/code",
		url.Values{"email": {email}, "code": {p.mail.lastCode(t)}})

	cookie := in.session()
	if cookie == nil {
		t.Fatalf("could not sign %s in", email)
	}
	return cookie
}

func onlySourceID(t *testing.T, p *testPortal) string {
	t.Helper()

	p.sources.mu.Lock()
	defer p.sources.mu.Unlock()
	if len(p.sources.rows) != 1 {
		t.Fatalf("there are %d sources, want exactly one", len(p.sources.rows))
	}
	return p.sources.rows[0].ID
}

// The one sentence this page exists to say. A customer who registers a source
// and is not told this discovers it from a send that failed, days later.
func TestANewSourceSaysItIsWaitingForApproval(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")

	made := post(t, p, "/sources", url.Values{"name": {"acme-billing"}}, cookie)
	if made.status != http.StatusSeeOther {
		t.Fatalf("POST /sources = %d\n%s", made.status, made.body)
	}

	list := get(t, p, "/sources", cookie)
	if !strings.Contains(strings.ToLower(list.body), "waiting for approval") {
		t.Errorf("the source list does not say the source is waiting:\n%s", list.body)
	}
	if strings.Contains(list.body, "Sending") {
		t.Error("a source nobody approved is shown as sending")
	}
}

// Ownership, from the outside. Somebody else's source is not found, and the
// answer does not differ from an id that never existed.
func TestACustomerCannotOpenSomebodyElsesSource(t *testing.T) {
	p := newTestPortal(t)

	mine := signedIn(t, p, "me@acme.test")
	post(t, p, "/sources", url.Values{"name": {"mine"}}, mine)
	id := onlySourceID(t, p)

	theirs := signedIn(t, p, "them@acme.test")
	got := get(t, p, "/sources/"+id, theirs)
	missing := get(t, p, "/sources/01K0SRC0000000000000000000", theirs)

	if got.status != missing.status {
		t.Errorf("somebody else's source answered %d and a missing one %d -- "+
			"the difference says the id exists", got.status, missing.status)
	}
	if strings.Contains(got.body, "mine") {
		t.Error("somebody else's source name was rendered")
	}
}

func TestTheSourceListNeedsASession(t *testing.T) {
	p := newTestPortal(t)

	if got := get(t, p, "/sources"); got.status != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect to sign in", got.status)
	}
}

// An account with nothing is the ordinary first screen, and it has to invite
// rather than look broken.
func TestAnEmptySourceListInvites(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")

	got := get(t, p, "/sources", cookie)
	if !strings.Contains(got.body, "Register your first source") {
		t.Errorf("the empty list does not offer anything to do:\n%s", got.body)
	}
}

// A source with no name is refused by the domain, and the page says so instead
// of redirecting to a list that did not change.
func TestASourceWithNoNameIsRefusedOnThePage(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")

	got := post(t, p, "/sources", url.Values{"name": {"  "}}, cookie)
	if got.status == http.StatusSeeOther {
		t.Fatal("a source with no name was accepted")
	}
	if !strings.Contains(strings.ToLower(got.body), "name") {
		t.Errorf("the page does not say what was wrong:\n%s", got.body)
	}
}

// --- keys ------------------------------------------------------------------

func onlyKeyID(t *testing.T, p *testPortal) string {
	t.Helper()

	p.keys.mu.Lock()
	defer p.keys.mu.Unlock()
	if len(p.keys.keys) != 1 {
		t.Fatalf("there are %d keys, want exactly one", len(p.keys.keys))
	}
	return p.keys.keys[0].ID.String()
}

// theKeyOn finds the key in a page by the prefix every key starts with.
func theKeyOn(t *testing.T, body string) string {
	t.Helper()

	i := strings.Index(body, "srosha_sk_")
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if end := strings.IndexAny(rest, "< \n\t"); end > 0 {
		return rest[:end]
	}
	return rest
}

// aSourceOfMine signs somebody in, registers one source and hands back both.
func aSourceOfMine(t *testing.T, p *testPortal, email string) (*http.Cookie, string) {
	t.Helper()

	cookie := signedIn(t, p, email)
	if got := post(t, p, "/sources", url.Values{"name": {"acme"}}, cookie); got.status !=
		http.StatusSeeOther {
		t.Fatalf("POST /sources = %d", got.status)
	}
	return cookie, onlySourceID(t, p)
}

// The key is shown once, on the page that made it, and never again. A page that
// showed it twice would mean srosha had kept it.
func TestAKeyIsShownOnceAndNeverAgain(t *testing.T) {
	p := newTestPortal(t)
	cookie, id := aSourceOfMine(t, p, "a@acme.test")

	issued := post(t, p, "/sources/"+id+"/keys", url.Values{"label": {"laptop"}}, cookie)
	if issued.status != http.StatusOK {
		t.Fatalf("POST keys = %d\n%s", issued.status, issued.body)
	}

	key := theKeyOn(t, issued.body)
	if key == "" {
		t.Fatal("the page that issues a key does not show it")
	}

	again := get(t, p, "/sources/"+id+"/keys", cookie)
	if strings.Contains(again.body, key) {
		t.Error("the key is on the list page, so it was kept somewhere")
	}
	for _, stored := range p.keys.hashes {
		if strings.Contains(stored, key) {
			t.Error("the key itself was stored, not a hash of it")
		}
	}
}

func TestRevokingAKeyTakesItOutOfUse(t *testing.T) {
	p := newTestPortal(t)
	cookie, id := aSourceOfMine(t, p, "a@acme.test")

	post(t, p, "/sources/"+id+"/keys", url.Values{"label": {"laptop"}}, cookie)
	keyID := onlyKeyID(t, p)

	res := post(t, p, "/sources/"+id+"/keys/"+keyID+"/revoke", url.Values{}, cookie)
	if res.status != http.StatusSeeOther {
		t.Fatalf("revoke = %d", res.status)
	}

	list := get(t, p, "/sources/"+id+"/keys", cookie)
	if !strings.Contains(list.body, "Revoked") {
		t.Errorf("the list does not show the key as revoked:\n%s", list.body)
	}
	if strings.Contains(list.body, ">Revoke<") {
		t.Error("a revoked key is still offered for revoking")
	}
}

// Ownership again, on the route that hands out credentials. Nobody issues a key
// against a source they do not own, however they reach the url.
func TestNobodyIssuesAKeyOnSomebodyElsesSource(t *testing.T) {
	p := newTestPortal(t)
	_, id := aSourceOfMine(t, p, "me@acme.test")

	stranger := signedIn(t, p, "them@acme.test")
	got := post(t, p, "/sources/"+id+"/keys", url.Values{"label": {"theirs"}}, stranger)

	if got.status == http.StatusOK && theKeyOn(t, got.body) != "" {
		t.Fatal("a stranger was handed a key for somebody else's source")
	}
	p.keys.mu.Lock()
	defer p.keys.mu.Unlock()
	if len(p.keys.keys) != 0 {
		t.Errorf("%d keys were written despite the refusal", len(p.keys.keys))
	}
}

// --- senders and the callback ----------------------------------------------

func theSecretOn(t *testing.T, body string) string {
	t.Helper()

	i := strings.Index(body, `class="k"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	open := strings.Index(rest, ">")
	close := strings.Index(rest, "</code>")
	if open < 0 || close < 0 || close <= open {
		return ""
	}
	return strings.TrimSpace(rest[open+1 : close])
}

// A source is configured while it waits, not after. This is the whole reason
// source.Service.Manage exists.
func TestASourceCanBeConfiguredBeforeItIsApproved(t *testing.T) {
	p := newTestPortal(t)
	cookie, id := aSourceOfMine(t, p, "a@acme.test")

	res := post(t, p, "/sources/"+id+"/callback",
		url.Values{"url": {"https://acme.test/hooks/srosha"}}, cookie)

	if res.status != http.StatusOK && res.status != http.StatusSeeOther {
		t.Fatalf("callback on an unapproved source = %d\n%s", res.status, res.body)
	}
	if strings.Contains(strings.ToLower(res.body), "not active") {
		t.Error("configuring an unapproved source was refused for being unapproved")
	}
}

// The signing secret is handed over once, like a key.
func TestTheCallbackSecretIsShownOnce(t *testing.T) {
	p := newTestPortal(t)
	cookie, id := aSourceOfMine(t, p, "a@acme.test")

	made := post(t, p, "/sources/"+id+"/callback",
		url.Values{"url": {"https://acme.test/hooks/srosha"}}, cookie)

	secret := theSecretOn(t, made.body)
	if secret == "" {
		t.Fatalf("registering a callback did not show its signing secret:\n%s", made.body)
	}

	again := get(t, p, "/sources/"+id+"/callback", cookie)
	if strings.Contains(again.body, secret) {
		t.Error("the secret is on the page a second time, so it was kept")
	}
}

// Ownership on both identity routes. These two use cases take a source id and
// check nothing about who is asking, so the handler has to.
func TestIdentityPagesRefuseSomebodyElsesSource(t *testing.T) {
	p := newTestPortal(t)
	_, id := aSourceOfMine(t, p, "me@acme.test")
	stranger := signedIn(t, p, "them@acme.test")

	for _, path := range []string{"/senders", "/callback"} {
		got := get(t, p, "/sources/"+id+path, stranger)
		if got.status != http.StatusNotFound {
			t.Errorf("GET %s as a stranger = %d, want 404", path, got.status)
		}
	}

	set := post(t, p, "/sources/"+id+"/callback",
		url.Values{"url": {"https://evil.test/hook"}}, stranger)
	if set.status != http.StatusNotFound {
		t.Errorf("a stranger set somebody else's callback: %d", set.status)
	}
}
