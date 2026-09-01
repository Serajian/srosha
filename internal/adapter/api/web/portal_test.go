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

func (m *memUsers) List(_ context.Context, limit int32) ([]user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]user.User, 0, len(m.rows))
	for _, u := range m.rows {
		if int32(len(out)) >= limit {
			break
		}
		out = append(out, *u)
	}
	return out, nil
}

func (m *memUsers) UpdateRole(_ context.Context, u *user.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[u.Email]
	if !ok {
		return errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
	}
	row.Role = u.Role
	row.UpdatedAt = u.UpdatedAt
	return nil
}

func (m *memUsers) SetActive(_ context.Context, u *user.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[u.Email]
	if !ok {
		return errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
	}
	row.IsActive = u.IsActive
	row.UpdatedAt = u.UpdatedAt
	return nil
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

	// readFails, when set, is what ReadByID answers instead of looking. It
	// stands in for a database that will not answer, which is a different
	// thing from an id nothing matches and must not be reported as one.
	readFails error
}

// breakReads makes every ReadByID fail with something that is not a
// not-found.
func (m *memSources) breakReads(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readFails = err
}

func (m *memSources) Create(_ context.Context, s *source.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, s)
	return nil
}

// UpdateSettings writes only the three columns the real statement writes. The
// fake mirroring that is the point: a test that saved the whole struct would
// pass even if the ceiling had been carried in.
func (m *memSources) UpdateSettings(_ context.Context, s *source.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.ID == s.ID {
			row.Name = s.Name
			row.Description = s.Description
			row.DefaultAddresses = s.DefaultAddresses
			row.UpdatedAt = s.UpdatedAt
			return nil
		}
	}
	return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
}

// ReadByID hands back a COPY, never the stored pointer.
//
// A real repository builds a struct from a row, so a caller that mutates what
// it read changes nothing until it writes. Handing out the pointer made the
// fake the opposite: Approve mutating the source it read would have "landed"
// with no UpdateReview at all, and TestApprovingLetsASourceLeaveTheQueue would
// have passed on a use case that never wrote anything. fakeSources in the use
// case package was fixed for this; this one, which the branch's only
// end-to-end suite runs on, was missed.
func (m *memSources) ReadByID(_ context.Context, id string) (*source.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readFails != nil {
		return nil, m.readFails
	}
	for _, s := range m.rows {
		if s.ID == id {
			got := *s
			return &got, nil
		}
	}
	return nil, errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
}

func (m *memSources) UpdateReview(_ context.Context, s *source.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.ID == s.ID {
			row.IsActive = s.IsActive
			row.ApprovedAt = s.ApprovedAt
			row.ReviewedAt = s.ReviewedAt
			row.ReviewNote = s.ReviewNote
			row.UpdatedAt = s.UpdatedAt
			return nil
		}
	}
	return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
}

func (m *memSources) ListForReview(_ context.Context, limit int32) ([]source.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []source.Source{}
	for _, s := range m.rows {
		if int32(len(out)) >= limit {
			break
		}
		if !s.IsReviewed() {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (m *memSources) ListAll(_ context.Context, limit int32) ([]source.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []source.Source{}
	for _, s := range m.rows {
		if int32(len(out)) >= limit {
			break
		}
		out = append(out, *s)
	}
	return out, nil
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

// List satisfies usecase.AuditLog, newest first and capped at limit.
func (m *memAudit) List(_ context.Context, limit int32) ([]usecase.AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.entries)
	if int32(n) > limit {
		n = int(limit)
	}
	out := make([]usecase.AuditEntry, n)
	for i := 0; i < n; i++ {
		out[i] = m.entries[len(m.entries)-1-i]
	}
	return out, nil
}

// ListByTarget mirrors postgres's own statement -- target_type, target_id and
// the given verb set, newest first, capped at limit -- for the same reason
// usecase_test's own auditLog fake does: a fake that ignored the verb list
// would let the admin surface's guard on /sources/:id go untested by
// TestASourcesOwnHistoryNeverShowsACustomerAddress in admin_test.go.
func (m *memAudit) ListByTarget(
	_ context.Context, targetType, targetID string, verbs []string, limit int32,
) ([]usecase.AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	allowed := make(map[string]bool, len(verbs))
	for _, v := range verbs {
		allowed[v] = true
	}

	var matched []usecase.AuditEntry
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := m.entries[i]
		if e.TargetType == targetType && e.TargetID == targetID && allowed[e.Verb] {
			matched = append(matched, e)
		}
	}
	if int32(len(matched)) > limit {
		matched = matched[:limit]
	}
	return matched, nil
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
	n    int
	rows map[string][]credential.Credential
}

func (m *memSenders) Register(
	_ context.Context, sourceID string, reg usecase.CredentialRegistration,
) (*credential.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// An id and a live flag, because the page now has buttons that need both.
	// Restore rather than a struct literal: whether an identity is live is the
	// entity's own state, not a field, and this fake must not be able to set it
	// in a way the real one cannot.
	m.n++
	c := credential.Restore(credential.Snapshot{
		ID:       shared.ID(fmt.Sprintf("01K0CRED00000000000000%04d", m.n)),
		SourceID: sourceID,
		Name:     reg.Name,
		Channel:  reg.Channel,
		IsActive: true,
	})
	m.rows[sourceID] = append(m.rows[sourceID], *c)
	return c, nil
}

func (m *memSenders) Deactivate(
	ctx context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	return m.setActive(ctx, sourceID, id, false)
}

func (m *memSenders) Activate(
	ctx context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	return m.setActive(ctx, sourceID, id, true)
}

func (m *memSenders) setActive(
	_ context.Context, sourceID string, id shared.ID, on bool,
) (*credential.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for i := range m.rows[sourceID] {
		if m.rows[sourceID][i].ID != id {
			continue
		}
		if on {
			m.rows[sourceID][i].Activate(now)
		} else {
			m.rows[sourceID][i].Deactivate(now)
		}
		return &m.rows[sourceID][i], nil
	}
	return nil, errs.NotFoundErr("no such sender")
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
	senders *memSenders
	keys    *memKeys
	audit   *memAudit
}

func newTestPortal(t *testing.T) *testPortal {
	t.Helper()

	users := &memUsers{rows: map[string]*user.User{}}
	senders := &memSenders{rows: map[string][]credential.Credential{}}
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
	gate := usecase.NewGate(audit, nil, ids, now)

	useSources := usecase.NewSources(sources, gate, ids, now)
	keys := &memKeys{}

	handler, err := web.NewPortal(web.PortalDeps{
		SignIn:       signIn,
		Sources:      useSources,
		Keys:         usecase.NewKeys(keys, useSources, &mintKeys{}, gate, ids, now),
		Senders:      senders,
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
		Server: srv, mail: mail, users: users, sources: sources,
		senders: senders, keys: keys, audit: audit,
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

// sessionNamed is the cookie that signs somebody in on one surface, or nil.
//
// The name is asked for rather than assumed: each surface writes its own, and
// a helper that accepted either would let one surface set the other's cookie
// without a single test noticing.
func (a answer) sessionNamed(name string) *http.Cookie {
	for _, c := range a.cookies {
		if c.Name == name && c.Value != "" {
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
	cookie := in.sessionNamed(web.PortalCookieName)
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
	if got.sessionNamed(web.PortalCookieName) != nil {
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

	cookie := in.sessionNamed(web.PortalCookieName)
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

// A source with no default address and no permission for a custom one cannot
// possibly be approved -- not by waiting, and not by an operator, who cannot
// add an address on the customer's behalf either. Telling this customer to
// simply wait would be the same silent failure review_note exists to end,
// just reached a different way: nothing here will ever move.
func TestAWaitingSourceWithNoAddressIsToldToAddOne(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{"name": {"acme-billing"}}, cookie)
	id := onlySourceID(t, p)

	got := get(t, p, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	if !strings.Contains(got.body, "nobody can approve it yet") {
		t.Errorf("a source with nowhere to send does not tell the customer to add one:\n%s",
			got.body)
	}
	if !strings.Contains(got.body, `href="/sources/`+id+`/edit"`) {
		t.Errorf("the message does not point at the edit page:\n%s", got.body)
	}
	if strings.Contains(got.body, "it starts working the moment") {
		t.Error("a source with nowhere to send still says it only needs to wait")
	}
}

// A source that already has somewhere to send gets the ordinary message: it
// really is only waiting.
func TestAWaitingSourceWithAnAddressGetsTheOrdinaryMessage(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{
		"name": {"acme-billing"}, "channel": {"email"}, "address": {"ops@acme.test"},
	}, cookie)
	id := onlySourceID(t, p)

	got := get(t, p, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	if !strings.Contains(got.body, "it starts working the moment") {
		t.Errorf("a source with an address does not get the ordinary waiting message:\n%s",
			got.body)
	}
	if strings.Contains(got.body, "nobody can approve it yet") {
		t.Error("a source that already has an address is still told to add one")
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

// A refusal a customer cannot read is a source that silently never works --
// the failure the "waiting for approval" message exists to avoid.
func TestARefusedSourceShowsItsReason(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{"name": {"acme-billing"}}, cookie)
	id := onlySourceID(t, p)

	at := time.Now().UTC()
	if err := p.sources.rows[0].Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	got := get(t, p, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	if !strings.Contains(got.body, "no working address") {
		t.Error("the customer is not told why")
	}
	if strings.Contains(strings.ToLower(got.body), "waiting for approval") {
		t.Error("a refused source still says it is waiting")
	}
}

// The list is the first thing a customer sees. If it still says "waiting"
// for a source that was refused or suspended weeks ago, the fix on the
// detail page never mattered -- nobody opens a source that claims to be
// fine already.
func TestARefusedOrSuspendedSourceDoesNotReadAsWaitingOnTheList(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{"name": {"refused-one"}}, cookie)
	post(t, p, "/sources", url.Values{"name": {"suspended-one"}}, cookie)

	p.sources.mu.Lock()
	if len(p.sources.rows) != 2 {
		p.sources.mu.Unlock()
		t.Fatalf("there are %d sources, want exactly two", len(p.sources.rows))
	}
	refused, suspended := p.sources.rows[0], p.sources.rows[1]
	p.sources.mu.Unlock()

	now := time.Now().UTC()
	if err := refused.Refuse("no working address", now); err != nil {
		t.Fatalf("Refuse: %v", err)
	}
	suspended.AllowCustomAddress = true // otherwise Approve refuses: nowhere to send
	if err := suspended.Approve(now); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := suspended.Suspend(now); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	got := get(t, p, "/sources", cookie)
	whole(t, "/sources", got)

	if strings.Contains(strings.ToLower(got.body), "waiting for approval") {
		t.Error("a refused or suspended source still reads as waiting for approval")
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

// --- getting anywhere ------------------------------------------------------

// The bug this catches shipped once: /sources existed and nothing linked to it,
// so a customer signed in and had no way to reach the thing they came for.
func TestEveryPageBehindTheGuardCanReachTheOthers(t *testing.T) {
	p := newTestPortal(t)
	cookie, id := aSourceOfMine(t, p, "a@acme.test")

	for _, path := range []string{
		"/", "/sources", "/sources/new",
		"/sources/" + id, "/sources/" + id + "/keys",
		"/sources/" + id + "/senders", "/sources/" + id + "/callback",
	} {
		got := get(t, p, path, cookie)
		whole(t, path, got)

		for _, link := range []string{`href="/sources"`, `action="/signout"`} {
			if !strings.Contains(got.body, link) {
				t.Errorf("%s has no %s -- somebody lands here and is stuck", path, link)
			}
		}
	}
}

// whole asserts a page actually finished.
//
// This exists because the first version of the navigation shipped a template
// that referenced a field the sign-in pages did not have. html/template refuses
// a missing field, gin records the error on the context and aborts, and the
// browser gets a 200 with the page cut off mid-tag. The sign-in form was gone
// and the test that should have caught it passed -- it asserted the navigation
// was absent, and it was absent because the page never reached it.
//
// Asserting a page is whole is therefore not ceremony. It is the only check
// that fails when a template stops halfway.
func whole(t *testing.T, path string, a answer) {
	t.Helper()

	if a.status != http.StatusOK {
		t.Fatalf("GET %s = %d", path, a.status)
	}
	if !strings.Contains(a.body, "</html>") {
		t.Fatalf("GET %s stopped before the end of the page:\n…%s",
			path, tail(a.body, 200))
	}
	if !strings.Contains(a.body, "</main>") {
		t.Errorf("GET %s has no main element -- it stopped inside the layout", path)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// And a page nobody has signed in to must not offer links it cannot honor --
// while still being a page.
func TestTheSignInPagesAreWholeAndHaveNoNavigation(t *testing.T) {
	p := newTestPortal(t)

	for path, mustHave := range map[string]string{
		"/signin":      "Send the code",
		"/signin/code": "Sign in",
	} {
		got := get(t, p, path)
		whole(t, path, got)

		if !strings.Contains(got.body, mustHave) {
			t.Errorf("GET %s does not carry its own form", path)
		}
		if strings.Contains(got.body, `class="nav"`) {
			t.Errorf("%s shows navigation to somebody who is not signed in", path)
		}
	}
}

// The one that matters. The form has no field for the ceiling, so this posts
// one anyway -- which is what an attacker does, and what a well-meaning later
// change to the form would do by accident.
func TestPostingWhatIsOursChangesNothing(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{"name": {"acme-billing"}}, cookie)
	id := onlySourceID(t, p)

	before := p.sources.rows[0]
	before.MaxPriority = shared.PriorityCritical
	before.AllowCustomAddress = true

	done := post(t, p, "/sources/"+id+"/edit", url.Values{
		"name":                 {"renamed"},
		"description":          {"pages the on-call"},
		"max_priority":         {"CRITICAL"},
		"allow_custom_address": {"true"},
		"is_active":            {"true"},
		"approved_at":          {"2026-01-01T00:00:00Z"},
		"owner_user_id":        {"01K0ACCT0000000000000000AC"},
	}, cookie)
	if done.status != http.StatusSeeOther {
		t.Fatalf("POST edit = %d\n%s", done.status, done.body)
	}

	got := p.sources.rows[0]
	if got.Name != "renamed" || got.Description != "pages the on-call" {
		t.Errorf("the edit did not take: %q / %q", got.Name, got.Description)
	}
	if got.IsActive {
		t.Error("a posted is_active switched the source on")
	}
	if got.ApprovedAt != nil {
		t.Error("a posted approved_at was written")
	}
	if got.MaxPriority != shared.PriorityCritical {
		t.Errorf("max_priority was overwritten with %v", got.MaxPriority)
	}
	if !got.AllowCustomAddress {
		t.Error("allow_custom_address was overwritten")
	}
}

// A default address outliving its usefulness is the ordinary reason to edit a
// source, so replacing one has to work rather than only adding another.
func TestADefaultAddressCanBeChanged(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{
		"name": {"acme-billing"}, "channel": {"email"}, "address": {"old@acme.test"},
	}, cookie)
	id := onlySourceID(t, p)

	post(t, p, "/sources/"+id+"/edit", url.Values{
		"name": {"acme-billing"}, "channel": {"email"}, "address": {"new@acme.test"},
	}, cookie)

	if got := p.sources.rows[0].DefaultAddresses[shared.ChannelEmail]; got != "new@acme.test" {
		t.Errorf("address = %q", got)
	}
}

// Somebody else's source is not editable, and says what a missing one says.
func TestAStrangerCannotEditASource(t *testing.T) {
	p := newTestPortal(t)

	mine := signedIn(t, p, "me@acme.test")
	post(t, p, "/sources", url.Values{"name": {"mine"}}, mine)
	id := onlySourceID(t, p)

	theirs := signedIn(t, p, "them@acme.test")

	if got := get(t, p, "/sources/"+id+"/edit", theirs); got.status != http.StatusNotFound {
		t.Errorf("GET their edit page = %d, want 404", got.status)
	}

	post(t, p, "/sources/"+id+"/edit", url.Values{"name": {"theirs now"}}, theirs)
	if p.sources.rows[0].Name != "mine" {
		t.Errorf("a stranger renamed it to %q", p.sources.rows[0].Name)
	}
}

// Switching a sender off is not deleting it. A source whose bot token was
// withdrawn needs the row still there when the new one arrives -- and after an
// incident, when it was switched off is the first question asked.
func TestASenderSwitchedOffIsStillThere(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{"name": {"acme-billing"}}, cookie)
	id := onlySourceID(t, p)

	post(t, p, "/sources/"+id+"/senders", url.Values{
		"channel": {"telegram"}, "name": {"our-support-bot"}, "secret": {"a-token"},
	}, cookie)

	senderID := p.senders.rows[id][0].ID

	off := post(t, p, "/sources/"+id+"/senders/"+senderID.String()+"/off", nil, cookie)
	if off.status != http.StatusSeeOther {
		t.Fatalf("POST off = %d\n%s", off.status, off.body)
	}
	if p.senders.rows[id][0].IsActive() {
		t.Error("the sender is still in use")
	}
	if len(p.senders.rows[id]) != 1 {
		t.Fatalf("switching off removed the row: %d left", len(p.senders.rows[id]))
	}

	page := get(t, p, "/sources/"+id+"/senders", cookie)
	if !strings.Contains(page.body, "our-support-bot") {
		t.Error("a switched-off sender vanished from the page")
	}

	on := post(t, p, "/sources/"+id+"/senders/"+senderID.String()+"/on", nil, cookie)
	if on.status != http.StatusSeeOther {
		t.Fatalf("POST on = %d", on.status)
	}
	if !p.senders.rows[id][0].IsActive() {
		t.Error("the sender did not come back")
	}
}

// A stranger cannot reach the switch, for the same reason they cannot reach the
// page it is on.
func TestAStrangerCannotSwitchASenderOff(t *testing.T) {
	p := newTestPortal(t)

	mine := signedIn(t, p, "me@acme.test")
	post(t, p, "/sources", url.Values{"name": {"mine"}}, mine)
	id := onlySourceID(t, p)
	post(t, p, "/sources/"+id+"/senders", url.Values{
		"channel": {"telegram"}, "name": {"our-support-bot"}, "secret": {"a-token"},
	}, mine)
	senderID := p.senders.rows[id][0].ID

	theirs := signedIn(t, p, "them@acme.test")
	got := post(t, p, "/sources/"+id+"/senders/"+senderID.String()+"/off", nil, theirs)

	if got.status != http.StatusNotFound {
		t.Errorf("POST their switch = %d, want 404", got.status)
	}
	if !p.senders.rows[id][0].IsActive() {
		t.Error("a stranger switched somebody else's sender off")
	}
}

// The form comes back filled in with what is already there. A blank edit form
// is a form that silently clears whatever it does not show.
func TestTheEditFormArrivesFilledIn(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{
		"name": {"acme-billing"}, "channel": {"email"}, "address": {"billing@acme.test"},
	}, cookie)
	id := onlySourceID(t, p)

	got := get(t, p, "/sources/"+id+"/edit", cookie)
	whole(t, "/sources/"+id+"/edit", got)

	for _, want := range []string{"acme-billing", "billing@acme.test", `name="description"`} {
		if !strings.Contains(got.body, want) {
			t.Errorf("the form does not carry %q", want)
		}
	}
}

// The other half of TestTheAdminSurfaceWritesItsOwnCookie: the portal keeps
// its own name too, so the two can never collapse into one cookie again.
func TestThePortalWritesItsOwnCookie(t *testing.T) {
	p := newTestPortal(t)

	post(t, p, "/signin", url.Values{"email": {"a@acme.test"}})
	in := post(t, p, "/signin/code",
		url.Values{"email": {"a@acme.test"}, "code": {p.mail.lastCode(t)}})

	if in.sessionNamed(web.AdminCookieName) != nil {
		t.Error("the portal set the admin surface's cookie")
	}
	if in.sessionNamed(web.PortalCookieName) == nil {
		t.Fatal("the portal set no session cookie of its own")
	}
}
