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
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

// The admin surface is tested over the REAL sign-in and Operators use cases,
// the same way portal_test.go tests the portal: a fake of web.Operators would
// only prove the handlers call something, and what matters here is the rule
// underneath -- the guard, the role check inside the use case, the domain's
// own refusals -- reaching the page unchanged.
//
// memUsers, memCodes, memSessions, memMailer, memSources and memAudit are
// portal_test.go's. Reused here rather than redeclared: they are already
// exactly what this surface's fakes look like, tested by the portal's own
// suite, and a second copy would be two things to keep in sync instead of
// one. memNotifications and memDeliveries are new -- the portal has no use
// for either.

// memNotifications holds a WHOLE notification, body and all, and hands
// ListForOperator the aggregate the real statement selects.
//
// It used to answer nil to everything, which meant /sources/:id/log was only
// ever rendered empty and the spec's content guarantee -- "asserted on the
// rendered page rather than on the struct" -- was asserted on neither. A fake
// that returns nothing proves nothing about what a page does with something.
//
// The body is here deliberately: the guarantee is that it cannot reach the
// page even when the store has it.
type memNotifications struct {
	mu   sync.Mutex
	rows map[string]*notification.Notification

	// deliveries is where the aggregate's counts come from, so the row a page
	// reads describes the deliveries a page can open.
	deliveries *memDeliveries
}

func (m *memNotifications) Create(_ context.Context, n *notification.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[n.ID.String()] = n
	return nil
}

func (m *memNotifications) ReadByID(
	_ context.Context, id shared.ID,
) (*notification.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.rows[id.String()]; ok {
		return n, nil
	}
	return nil, errs.NotFoundErr("notification not found").WithErr(notification.ErrNotFound)
}

func (*memNotifications) ReadByIdempotencyKey(
	context.Context, string, string,
) (*notification.Notification, error) {
	return nil, nil
}

func (*memNotifications) PageBySource(
	context.Context, string, time.Time, shared.Cursor,
) (shared.Pagination[notification.Notification], error) {
	return shared.Pagination[notification.Notification]{}, nil
}

func (*memNotifications) DeleteOlderThan(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

// ListForOperator is the aggregate the real statement selects: no Title and no
// Body, because OperatorRow has nowhere to put them. Built from the stored
// notification, so this fake cannot drift into answering something the row
// type could not hold.
func (m *memNotifications) ListForOperator(
	_ context.Context, sourceID string, limit int,
) ([]notification.OperatorRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []notification.OperatorRow
	for _, n := range m.rows {
		if n.SourceID != sourceID || len(out) >= limit {
			continue
		}
		ds, _ := m.deliveries.byNotification(n.ID)

		channels := make([]string, 0, len(ds))
		for i := range ds {
			channels = append(channels, string(ds[i].Recipient.Channel))
		}
		out = append(out, notification.OperatorRow{
			ID: n.ID, Channels: channels, Total: len(ds), CreatedAt: n.CreatedAt,
		})
	}
	return out, nil
}

// memDeliveries holds real deliveries, with a real address in the clear, for
// the same reason memNotifications holds a body: what the page must never
// show has to exist before "it is not on the page" means anything.
type memDeliveries struct {
	mu   sync.Mutex
	rows []delivery.Delivery
}

func (m *memDeliveries) CreateByList(_ context.Context, ds []delivery.Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, ds...)
	return nil
}

func (*memDeliveries) ReadByID(context.Context, shared.ID) (*delivery.Delivery, error) {
	return nil, errs.NotFoundErr("delivery not found").WithErr(delivery.ErrNotFound)
}

func (m *memDeliveries) ListByNotificationID(
	_ context.Context, id shared.ID,
) ([]delivery.Delivery, error) {
	return m.byNotification(id)
}

// byNotification hands back copies, never the stored values -- a page that
// mutated what it read would otherwise change the fixture under the next
// assertion.
func (m *memDeliveries) byNotification(id shared.ID) ([]delivery.Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []delivery.Delivery
	for i := range m.rows {
		if m.rows[i].NotificationID == id {
			out = append(out, m.rows[i])
		}
	}
	return out, nil
}

func (*memDeliveries) ClaimStale(
	context.Context, time.Duration, time.Duration, int,
) ([]delivery.Delivery, error) {
	return nil, nil
}

func (*memDeliveries) ClaimAnnouncement(context.Context, shared.ID, time.Time) (bool, error) {
	return false, nil
}

func (*memDeliveries) Release(context.Context, *delivery.Delivery) error { return nil }

func (*memDeliveries) PageByNotificationID(
	context.Context, shared.ID, shared.Cursor,
) (shared.Pagination[delivery.Delivery], error) {
	return shared.Pagination[delivery.Delivery]{}, nil
}

func (*memDeliveries) Update(context.Context, *delivery.Delivery) error { return nil }

// memCredentials is credential.Repository's stand-in -- the raw port, not
// portal_test.go's memSenders, which implements the narrower shape
// usecase.Credentials exposes to the portal. Operators takes the repository
// itself, so this surface's tests seed it directly rather than through a
// registration flow.
type memCredentials struct {
	mu   sync.Mutex
	rows map[string][]credential.Credential
}

func (m *memCredentials) ListBySourceAndChannel(
	_ context.Context, sourceID string, c shared.Channel,
) ([]credential.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []credential.Credential
	for _, cr := range m.rows[sourceID] {
		if cr.Channel == c {
			out = append(out, cr)
		}
	}
	return out, nil
}

func (m *memCredentials) ListBySourceID(
	_ context.Context, sourceID string,
) ([]credential.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[sourceID], nil
}

func (m *memCredentials) ReadByID(
	_ context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows[sourceID] {
		if m.rows[sourceID][i].ID == id {
			got := m.rows[sourceID][i]
			return &got, nil
		}
	}
	return nil, errs.NotFoundErr("no such credential").WithErr(credential.ErrNotFound)
}

func (m *memCredentials) Deactivate(_ context.Context, c *credential.Credential) error {
	return m.save(c)
}

func (m *memCredentials) Activate(_ context.Context, c *credential.Credential) error {
	return m.save(c)
}

func (m *memCredentials) SetDefault(_ context.Context, c *credential.Credential) error {
	return m.save(c)
}

func (m *memCredentials) ClearDefault(
	context.Context, string, shared.Channel, time.Time,
) error {
	return nil
}

func (m *memCredentials) save(c *credential.Credential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows[c.SourceID] {
		if m.rows[c.SourceID][i].ID == c.ID {
			m.rows[c.SourceID][i] = *c
			return nil
		}
	}
	return errs.NotFoundErr("no such credential").WithErr(credential.ErrNotFound)
}

// add seeds one credential directly, standing in for a sender the customer
// already registered through the portal.
func (m *memCredentials) add(sourceID string, c credential.Credential) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[sourceID] = append(m.rows[sourceID], c)
}

// seed writes one audit row directly, standing in for an act somebody already
// took on the other listener -- the admin surface has no route that registers
// a source or issues a key, and those are exactly the acts whose actor is the
// customer.
func (m *memAudit) seed(actorEmail, verb string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, usecase.AuditEntry{
		ID:         shared.ID("01K0AUDIT000000000000000A"),
		At:         time.Now().UTC(),
		ActorID:    shared.ID("01K0OWNER00000000000000AA"),
		ActorEmail: actorEmail,
		Verb:       verb,
		TargetType: "source",
		TargetID:   "01K0SRCADMIN00000000000a",
	})
}

// setRole is the fixture's way of reaching in and changing a stored row
// directly -- standing in for "an operator already changed this, and now
// somebody's cookie is stale," which no route on this surface is a shortcut
// for.
func (m *memUsers) setRole(t *testing.T, email string, role user.Role) {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.rows[email]
	if !ok {
		t.Fatalf("no such user %q", email)
	}
	u.Role = role
}

type testAdmin struct {
	*httptest.Server

	// handler is the engine itself, kept beside the server so the boundary
	// test can read its route table rather than a list somebody maintains.
	handler web.AdminHandler

	mail          *memMailer
	users         *memUsers
	sources       *memSources
	audit         *memAudit
	credentials   *memCredentials
	notifications *memNotifications
	deliveries    *memDeliveries
}

// testAdminListLimit is generous enough that no ordinary fixture in this file
// truncates by accident. TestATruncatedListingSaysSo below builds its own
// admin with a limit small enough to trigger it on purpose.
const testAdminListLimit = 50

func newTestAdmin(t *testing.T) *testAdmin { return newTestAdminWithLimit(t, testAdminListLimit) }

func newTestAdminWithLimit(t *testing.T, listLimit int32) *testAdmin {
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
	credentials := &memCredentials{rows: map[string][]credential.Credential{}}
	deliveries := &memDeliveries{}
	notifications := &memNotifications{
		rows: map[string]*notification.Notification{}, deliveries: deliveries,
	}
	gate := usecase.NewGate(audit, nil, ids, now)

	ops := usecase.NewOperators(
		sources, users, notifications, deliveries, credentials, audit, gate, now, listLimit,
	)

	handler, err := web.NewAdmin(web.AdminDeps{
		SignIn:       signIn,
		Operators:    ops,
		SecureCookie: false,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("web.NewAdmin: %v", err)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testAdmin{
		Server: srv, handler: handler, mail: mail, users: users, sources: sources,
		audit: audit, credentials: credentials,
		notifications: notifications, deliveries: deliveries,
	}
}

// aget and apost are get and post's admin equivalents. Both reuse do -- the
// one thing in portal_test.go that already knows nothing about which surface
// it is talking to -- so nothing about the portal's own test file changes to
// support this one.
func aget(t *testing.T, a *testAdmin, path string, cookies ...*http.Cookie) answer {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, a.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return do(t, req, cookies)
}

func apost(
	t *testing.T, a *testAdmin, path string, form url.Values, cookies ...*http.Cookie,
) answer {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, a.URL+path, strings.NewReader(form.Encode()),
	)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(t, req, cookies)
}

// signedInAdmin runs the whole sign-in flow against the admin listener and
// hands back the cookie. Everybody starts a customer -- signedInAs is what
// promotes one afterwards.
func signedInAdmin(t *testing.T, a *testAdmin, email string) *http.Cookie {
	t.Helper()

	apost(t, a, "/signin", url.Values{"email": {email}})
	in := apost(t, a, "/signin/code",
		url.Values{"email": {email}, "code": {a.mail.lastCode(t)}})

	cookie := in.sessionNamed(web.AdminCookieName)
	if cookie == nil {
		t.Fatalf("could not sign %s in", email)
	}
	return cookie
}

// signedInAs signs somebody in and then sets their role on the stored row.
// After the sign-in, deliberately: everybody who has never been seen becomes
// a customer, and this is standing in for an operator having promoted them
// some time before today.
func signedInAs(t *testing.T, a *testAdmin, email string, role user.Role) *http.Cookie {
	t.Helper()

	cookie := signedInAdmin(t, a, email)
	a.users.setRole(t, email, role)
	return cookie
}

// aSourceInTheQueue registers one source waiting for a decision, with a
// default address -- most of this file's tests are about the review flow
// itself, and Approve now refuses a source with nowhere to send. The
// dedicated test for THAT guard builds its own source without one, below.
//
// The id is built from the name's first letter, so two sources in one test
// need two different first letters. Asserted rather than assumed: "sending"
// and "suspended" collided once, and the failure read as a filter bug rather
// than a fixture one.
func aSourceInTheQueue(t *testing.T, a *testAdmin, name string) string {
	t.Helper()

	id := "01K0SRCADMIN00000000000" + name[:1]
	if _, err := a.sources.ReadByID(context.Background(), id); err == nil {
		t.Fatalf("two sources in one test start with %q, so they share an id", name[:1])
	}

	now := time.Now().UTC()
	src, err := source.New(
		id,
		shared.ID("01K0OWNER00000000000000AA"),
		name,
		map[shared.Channel]string{shared.ChannelEmail: name + "@acme.test"},
		now,
	)
	if err != nil {
		t.Fatalf("source.New: %v", err)
	}
	if err := a.sources.Create(context.Background(), src); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return src.ID
}

// aSourceWithNoAddress registers one source waiting for a decision that has
// nowhere to send: no default address, custom addresses not allowed. It is
// what the "cannot be approved yet" page test needs, and the only caller
// that wants it -- every other test wants aSourceInTheQueue's reachable one.
func aSourceWithNoAddress(t *testing.T, a *testAdmin, name string) string {
	t.Helper()

	id := "01K0SRCADMIN00000000000" + name[:1]
	if _, err := a.sources.ReadByID(context.Background(), id); err == nil {
		t.Fatalf("two sources in one test start with %q, so they share an id", name[:1])
	}

	src, err := source.New(
		id, shared.ID("01K0OWNER00000000000000AA"), name, nil, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("source.New: %v", err)
	}
	if err := a.sources.Create(context.Background(), src); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return src.ID
}

// aMessageWithABody seeds one message and one delivery for a source, both
// carrying exactly what an operator must never be shown: a distinctive body
// and a full address. It hands back the message id, which is what opens the
// deliveries on the log page.
func aMessageWithABody(t *testing.T, a *testAdmin, sourceID, body, address string) string {
	t.Helper()

	at := time.Now().UTC()
	msg, err := notification.New(
		shared.ID("01K0MSGADMIN0000000000"+strings.ToUpper(sourceID[len(sourceID)-1:])+"AA"),
		notification.Origin{ID: sourceID, Name: "acme", MaxPriority: shared.PriorityHigh},
		notification.Request{Body: body, Priority: shared.PriorityNormal},
		at,
	)
	if err != nil {
		t.Fatalf("notification.New: %v", err)
	}
	if err := a.notifications.Create(context.Background(), msg); err != nil {
		t.Fatalf("seeding the message: %v", err)
	}

	var n int
	set, err := delivery.NewSet(
		msg.ID,
		[]shared.Recipient{{Channel: shared.ChannelEmail, Address: address}},
		nil,
		func() shared.ID {
			n++
			return shared.ID("01K0DLVADMIN0000000000" + string(rune('A'+n%26)) + "AA")
		},
		at,
	)
	if err != nil {
		t.Fatalf("delivery.NewSet: %v", err)
	}
	if err := a.deliveries.CreateByList(context.Background(), set); err != nil {
		t.Fatalf("seeding the delivery: %v", err)
	}
	return msg.ID.String()
}

// --- the boundary itself, which docs/ARCHITECTURE.md calls not optional ----

// The portal never mounts an admin route. A mounting mistake -- one that put
// review, people or audit on the public engine -- fails this test instead of
// shipping. docs/ARCHITECTURE.md calls this not optional: it is what replaces
// the compiler barrier a fourth binary would have given, now that both
// surfaces are structs in one package with the handlers of each in scope from
// the other.
//
// The routes come from the ADMIN ENGINE'S OWN TABLE, not from literals here.
// The version this replaces iterated {"/queue", "/people", "/audit"}, and
// "/queue" is a path on neither surface -- pathQueue is "/" -- so a third of
// what it checked was nothing, and every route that genuinely shares the
// portal's "/sources/:id/..." shape was absent. A list written by hand beside
// the thing it describes is a list that stops describing it.
//
// web.AdminPathsSharedWithThePortal carries the exemptions and the reason for
// each; web.AdminOnlyPaths is the floor, so a route dropped from NewAdmin
// cannot quietly shrink what is asserted.
func TestNoAdminRouteAnswersOnThePortal(t *testing.T) {
	a := newTestAdmin(t)
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	bothSurfaces := map[string]bool{}
	for _, path := range web.AdminPathsSharedWithThePortal {
		bothSurfaces[path] = true
	}

	// Concrete values for gin's parameters. A portal route that matched one of
	// these would answer something other than 404 and fail below, which is the
	// point -- "/sources/:id/approve" only collides once it is a real request.
	fill := strings.NewReplacer(
		":id", "01K0SRCADMIN00000000000a",
		":senderID", "01K0SNDADMIN000000000001",
		":keyID", "01K0KEYADMIN000000000001",
	)

	checked := map[string]bool{}
	for _, route := range web.AdminRouteTable(a.handler) {
		if bothSurfaces[route.Path] {
			continue
		}
		checked[route.Path] = true

		path := fill.Replace(route.Path)

		got := get(t, p, path, cookie)
		if route.Method == http.MethodPost {
			got = post(t, p, path, url.Values{}, cookie)
		}
		if got.status != http.StatusNotFound {
			t.Errorf("the portal answers %s %s with %d\n%s",
				route.Method, path, got.status, got.body)
		}
	}

	if len(checked) == 0 {
		t.Fatal("the admin engine handed back no routes -- this test checked nothing")
	}
	for _, want := range web.AdminOnlyPaths {
		if !checked[want] {
			t.Errorf("%s exists only on the admin surface and was not checked", want)
		}
	}
}

// The other half: a customer's session is refused by the admin guard. Their
// cookie is valid and it reaches this listener, because a cookie is not
// scoped by port.
func TestTheAdminSurfaceRefusesACustomer(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "me@acme.test", user.RoleCustomer)

	for _, path := range []string{"/", "/sources", "/people", "/audit"} {
		got := aget(t, a, path, cookie)
		if got.status == http.StatusOK {
			t.Errorf("a customer reached %s", path)
		}
	}
}

// An admin does the day-to-day and is refused on the pages that would tell
// them who has an account.
func TestAnAdminIsRefusedOnThePeoplePages(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	if got := aget(t, a, "/", cookie); got.status != http.StatusOK {
		t.Errorf("an admin cannot reach the queue: %d", got.status)
	}
	if got := aget(t, a, "/people", cookie); got.status == http.StatusOK {
		t.Error("an admin reached /people")
	}
	if got := apost(t, a, "/people/01K0ACCT0000000000000000AB/role",
		url.Values{"role": {"admin"}}, cookie); got.status == http.StatusOK {
		t.Error("an admin posted a role change")
	}
}

// /audit is the roster by another door: its rows carry actor_email, and the
// actor of a source registration or a key issue is the CUSTOMER. An admin
// reaching it would read every address /people was locked away to hide.
func TestAnAdminIsRefusedOnTheAuditLog(t *testing.T) {
	a := newTestAdmin(t)

	// A customer's own act, so the row that would be rendered carries a
	// customer's address rather than an operator's.
	admin := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	a.audit.seed("billing@acme.test", usecase.ActSourceCreate)

	if got := aget(t, a, "/audit", admin); got.status == http.StatusOK {
		t.Errorf("an admin reached /audit:\n%s", got.body)
	}
	if got := aget(t, a, "/audit", admin); strings.Contains(got.body, "billing@acme.test") {
		t.Error("an admin was shown a customer's address from the audit log")
	}

	top := signedInAs(t, a, "root@srosha.ir", user.RoleSuperAdmin)
	page := aget(t, a, "/audit", top)
	if page.status != http.StatusOK {
		t.Fatalf("a super_admin cannot reach /audit: %d", page.status)
	}
	if !strings.Contains(page.body, "billing@acme.test") {
		t.Errorf("the audit page does not show the actor it read:\n%s", page.body)
	}
}

// The diagram is every operator's, admin and super_admin alike. It was
// super_admin only for a day, and the boundary it still has is the one that
// matters: `operator`, so a customer holding a valid portal session does not
// reach it -- which is the whole reason that guard exists.
//
// It is also the one thing this surface serves that is NOT a page: no layout,
// no nav and no way to sign out, which is why TestEveryAdminPageIsWhole does
// not name it. What is asserted instead is that the document itself came back,
// and that it reaches nothing on the network to render -- see docs/CONFIG.md,
// "Pages and assets": the panel has to work where a font host does not answer.
func TestEveryOperatorReachesTheArchitectureDiagram(t *testing.T) {
	a := newTestAdmin(t)

	for _, role := range []user.Role{user.RoleAdmin, user.RoleSuperAdmin} {
		cookie := signedInAs(t, a, string(role)+"@srosha.ir", role)

		page := aget(t, a, "/architecture", cookie)
		if page.status != http.StatusOK {
			t.Fatalf("an operator whose role is %s cannot reach /architecture: %d",
				role, page.status)
		}
		if !strings.Contains(page.body, "<svg") {
			t.Errorf("/architecture answered a %s with no diagram in it", role)
		}
		for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com"} {
			if strings.Contains(page.body, host) {
				t.Errorf("the diagram reaches %s to render itself", host)
			}
		}
	}
}

// The half of the rule that did not move: a customer's session reaches this
// listener, because a cookie is not scoped by port, and `operator` is what
// turns it away.
func TestACustomerIsRefusedOnTheArchitectureDiagram(t *testing.T) {
	a := newTestAdmin(t)
	p := newTestPortal(t)
	cookie := signedIn(t, p, "billing@acme.test")

	if got := aget(t, a, "/architecture", cookie); got.status == http.StatusOK {
		t.Errorf("a customer read the architecture diagram:\n%s", got.body)
	}
}

// A source's own page is exactly where /audit's own boundary would be worth
// breaking to widen: this proves it stays narrow. The four operator verbs
// (approve, refuse, suspend, restore) are safe for an admin because their
// actor is always an operator, never the customer who owns the source --
// unlike source.create, whose actor_email IS the customer's address, the
// whole reason /audit itself is locked away above. An admin reaching
// /sources/:id must see the former and never the latter, on the very same
// source.
func TestASourcesOwnHistoryNeverShowsACustomerAddress(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	// "acme" makes aSourceInTheQueue build the id memAudit.seed hard-codes,
	// so the customer's row below lands on the very source this test reads.
	id := aSourceInTheQueue(t, a, "acme")

	// The customer's own act on this source -- nothing on this listener
	// writes one, registering a source is the portal's, so it stands in for
	// what a real gate wrote there.
	a.audit.seed("billing@acme.test", usecase.ActSourceCreate)

	// An operator's own decision, through the real path, on the same source.
	if res := apost(t, a, "/sources/"+id+"/approve", url.Values{}, cookie); res.status != http.StatusSeeOther {
		t.Fatalf("approve = %d\n%s", res.status, res.body)
	}

	page := aget(t, a, "/sources/"+id, cookie)
	if page.status != http.StatusOK {
		t.Fatalf("an admin cannot reach its own source's page: %d", page.status)
	}
	if strings.Contains(page.body, "billing@acme.test") {
		t.Errorf("a customer's address reached an admin's source page:\n%s", page.body)
	}
	if !strings.Contains(page.body, usecase.ActSourceApprove) {
		t.Errorf("the operator's own decision is missing from its history:\n%s", page.body)
	}
}

// Taking somebody's operator role takes effect on their next request, not
// their next sign-in. This is why the guard reads the users row rather than
// trusting the cookie, and without this test that is a comment.
func TestLosingTheRoleTakesEffectAtOnce(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	if got := aget(t, a, "/", cookie); got.status != http.StatusOK {
		t.Fatalf("an admin could not reach the queue: %d", got.status)
	}

	// The same person, demoted, holding the same cookie.
	a.users.setRole(t, "ops@srosha.ir", user.RoleCustomer)

	if got := aget(t, a, "/", cookie); got.status == http.StatusOK {
		t.Error("a demoted operator still reached the queue with their old cookie")
	}
}

// --- every page finishes rendering ------------------------------------------

// The bug whole() exists to catch: a page that stops mid-tag because a
// template referenced a field its view model did not have. Every admin page
// behind the guard is asserted whole, the same way the portal's are.
func TestEveryAdminPageIsWhole(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "root@srosha.ir", user.RoleSuperAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	for _, path := range []string{
		"/", "/sources", "/sources/" + id, "/sources/" + id + "/log",
		"/audit", "/people",
	} {
		got := aget(t, a, path, cookie)
		whole(t, path, got)

		if !strings.Contains(got.body, `action="/signout"`) {
			t.Errorf("%s has no way to sign out -- somebody lands here and is stuck", path)
		}
	}

	// /people/:id needs a real person id.
	personPage := aget(t, a, "/people/"+idOf(t, a, "root@srosha.ir"), cookie)
	whole(t, "/people/:id", personPage)
}

// A super_admin sees the /people and /audit links on an ordinary operator
// page; an admin does not. Both are still whole pages either way, and an
// admin is not shown a link that would only redirect them away.
//
// /architecture is deliberately NOT in that list and is asserted the other
// way below: it is every operator's, so an admin missing it would be a link
// withheld from somebody the route lets in.
func TestTheNavShowsPeopleAndAuditOnlyToASuperAdmin(t *testing.T) {
	a := newTestAdmin(t)

	admin := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	top := signedInAs(t, a, "root@srosha.ir", user.RoleSuperAdmin)

	topLinks := []string{`href="/people"`, `href="/audit"`}

	asAdmin := aget(t, a, "/", admin)
	whole(t, "/", asAdmin)
	for _, link := range topLinks {
		if strings.Contains(asAdmin.body, link) {
			t.Errorf("an admin's queue page offers %s", link)
		}
	}

	asTop := aget(t, a, "/", top)
	whole(t, "/", asTop)
	for _, link := range topLinks {
		if !strings.Contains(asTop.body, link) {
			t.Errorf("a super_admin's queue page does not offer %s", link)
		}
	}

	// The diagram is the one link both of them get. Asserted on both pages
	// rather than on the super_admin's alone, because the mistake this
	// catches is the link sliding back inside the {{if .SuperAdmin}} branch
	// it used to live in -- which nothing else here would notice.
	for who, page := range map[string]answer{"an admin": asAdmin, "a super_admin": asTop} {
		if !strings.Contains(page.body, `href="/architecture"`) {
			t.Errorf("%s's queue page does not offer the architecture diagram", who)
		}
	}
}

// Signing in shares its page types with the portal's chrome, not this
// surface's adminChrome -- see admin.go. Proving the sign-in pages are still
// whole, with no nav, is what proves that sharing is safe rather than merely
// convenient.
func TestTheAdminSignInPagesAreWholeAndHaveNoNavigation(t *testing.T) {
	a := newTestAdmin(t)

	for path, mustHave := range map[string]string{
		"/signin":      "Send the code",
		"/signin/code": "Sign in",
	} {
		got := aget(t, a, path)
		whole(t, path, got)

		if !strings.Contains(got.body, mustHave) {
			t.Errorf("GET %s does not carry its own form", path)
		}
		if strings.Contains(got.body, `class="nav"`) {
			t.Errorf("%s shows navigation to somebody who is not signed in", path)
		}
	}
}

// The delivery view carries neither the message body nor the address in the
// clear, asserted on the RENDERED PAGE and not on the struct -- which is what
// the spec asks for, and what nothing checked until now: both fakes answered
// nil, so this page was only ever rendered empty and the assertion had nothing
// to be false about.
//
// The struct half is real and is tested in the use case's own suite:
// OperatorMessage and OperatorDelivery have no field for a body. This is the
// other half -- that the template renders MaskedAddress and not Address, and
// that no part of the layout leaks either.
func TestTheDeliveryViewShowsNoBodyAndNoAddressInTheClear(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	const body = "your one-time code is 481902"
	const address = "veronica.mccallister@long-customer-domain.test"
	msg := aMessageWithABody(t, a, id, body, address)

	page := aget(t, a, "/sources/"+id+"/log?message="+msg, cookie)
	whole(t, "/sources/:id/log", page)

	// The page must actually be showing the delivery -- otherwise this test
	// passes on an empty table, which is exactly how it passed before.
	if !strings.Contains(page.body, msg) {
		t.Fatalf("the log page does not show the seeded message:\n%s", page.body)
	}
	if !strings.Contains(page.body, "<td>PENDING</td>") {
		t.Fatalf("the delivery row is not on the page, so nothing was asserted:\n%s", page.body)
	}

	if strings.Contains(page.body, address) {
		t.Errorf("the full address is on the page:\n%s", page.body)
	}
	if !strings.Contains(page.body, "ve…st") {
		t.Errorf("the address is not masked on the page:\n%s", page.body)
	}
	if strings.Contains(page.body, body) {
		t.Errorf("the message body is on the page:\n%s", page.body)
	}
	if strings.Contains(page.body, "481902") {
		t.Errorf("a one-time code from the body is on the page:\n%s", page.body)
	}
}

// The log page answers 404 for a source that is not there, the same as
// /sources/:id one click away. Messages returns an empty list for an id
// nothing matches -- true, and useless -- so without reading the source first
// a typo rendered a perfectly good log page for nothing.
func TestTheLogOfASourceThatIsNotThereIsNotFound(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	const ghost = "01K0SRCNOSUCH0000000000Z"
	if got := aget(t, a, "/sources/"+ghost, cookie); got.status != http.StatusNotFound {
		t.Fatalf("/sources/:id for an unknown id = %d, want 404", got.status)
	}
	if got := aget(t, a, "/sources/"+ghost+"/log", cookie); got.status != http.StatusNotFound {
		t.Errorf("/sources/:id/log for the same unknown id = %d, want 404", got.status)
	}
}

// A database that will not answer is a 500, not a 404.
//
// Every error on these routes used to be "no such source", which sends an
// operator hunting for a typo in an id that is perfectly correct, during an
// outage, and leaves nothing worth reading in the log afterwards.
func TestADatabaseThatWillNotAnswerIsNotAMissingSource(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	a.sources.breakReads(errs.InternalErr("the database is not answering"))

	for _, path := range []string{
		"/sources/" + id,
		"/sources/" + id + "/log",
	} {
		got := aget(t, a, path, cookie)
		if got.status == http.StatusNotFound {
			t.Errorf("%s answered 404 for a database that is down", path)
		}
		if got.status != http.StatusInternalServerError {
			t.Errorf("%s = %d, want 500", path, got.status)
		}
	}
}

// One source's log must not show another source's recipients. The page reads
// /sources/A/log?message=X and puts the answer under A's heading, so a message
// id belonging to B would have been B's recipients labeled as A's.
func TestOneSourcesLogWillNotOpenAnothersMessage(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	first := aSourceInTheQueue(t, a, "acme")
	second := aSourceInTheQueue(t, a, "beta")

	const address = "veronica.mccallister@long-customer-domain.test"
	theirs := aMessageWithABody(t, a, second, "a code for beta", address)

	page := aget(t, a, "/sources/"+first+"/log?message="+theirs, cookie)
	whole(t, "/sources/:id/log", page)

	if strings.Contains(page.body, "ve…st") {
		t.Errorf("one source's log opened another's deliveries:\n%s", page.body)
	}
	if !strings.Contains(strings.ToLower(page.body), "not one of this source") {
		t.Errorf("the page does not say why the message did not open:\n%s", page.body)
	}

	// And it still opens its own.
	mine := aMessageWithABody(t, a, first, "a code for acme", address)
	own := aget(t, a, "/sources/"+first+"/log?message="+mine, cookie)
	if !strings.Contains(own.body, "ve…st") {
		t.Errorf("a source's log will not open its own message:\n%s", own.body)
	}
}

// --- decisions ---------------------------------------------------------------

func TestApprovingLetsASourceLeaveTheQueue(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	res := apost(t, a, "/sources/"+id+"/approve", url.Values{}, cookie)
	if res.status != http.StatusSeeOther {
		t.Fatalf("approve = %d\n%s", res.status, res.body)
	}

	queue := aget(t, a, "/", cookie)
	if strings.Contains(queue.body, id) {
		t.Error("an approved source is still in the queue")
	}

	src := aget(t, a, "/sources/"+id, cookie)
	if !strings.Contains(strings.ToLower(src.body), "sending") {
		t.Errorf("the source page does not say it is sending:\n%s", src.body)
	}
}

// A refusal needs a reason. The domain refuses one with none, and the page
// says so rather than silently doing nothing.
func TestRefusingWithNoReasonIsRefused(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	res := apost(t, a, "/sources/"+id+"/refuse", url.Values{"note": {"  "}}, cookie)
	if res.status != http.StatusOK {
		t.Fatalf("refuse with no reason = %d, want the form again", res.status)
	}
	if !strings.Contains(strings.ToLower(res.body), "reason") {
		t.Errorf("the page does not say a reason is needed:\n%s", res.body)
	}

	queue := aget(t, a, "/", cookie)
	if !strings.Contains(queue.body, id) {
		t.Error("a source that was not refused left the queue")
	}
}

// An approved source cannot be refused -- only suspended -- and the page says
// exactly that, because that is the domain's own error message.
func TestAnApprovedSourceIsSuspendedNotRefused(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	apost(t, a, "/sources/"+id+"/approve", url.Values{}, cookie)

	res := apost(t, a, "/sources/"+id+"/refuse", url.Values{"note": {"changed my mind"}}, cookie)
	if res.status != http.StatusOK {
		t.Fatalf("refuse an approved source = %d", res.status)
	}
	if !strings.Contains(strings.ToLower(res.body), "suspend") {
		t.Errorf("the page does not point at suspending instead:\n%s", res.body)
	}

	src := aget(t, a, "/sources/"+id, cookie)
	if !strings.Contains(strings.ToLower(src.body), "sending") {
		t.Error("the refusal that was refused still switched the source off")
	}
}

// Suspend and restore are the way back for a source that already got through.
func TestSuspendingAndRestoringAnApprovedSource(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	apost(t, a, "/sources/"+id+"/approve", url.Values{}, cookie)
	apost(
		t,
		a,
		"/sources/"+id+"/suspend",
		url.Values{"note": {"complaint from a recipient"}},
		cookie,
	)

	suspended := aget(t, a, "/sources/"+id, cookie)
	if strings.Contains(strings.ToLower(suspended.body), "sending") {
		t.Error("a suspended source still reads as sending")
	}

	apost(t, a, "/sources/"+id+"/restore", url.Values{}, cookie)

	restored := aget(t, a, "/sources/"+id, cookie)
	if !strings.Contains(strings.ToLower(restored.body), "sending") {
		t.Error("a restored source does not read as sending")
	}
}

// /sources narrows to one state, which the spec's page list has said since
// the first draft and nothing did. Four sources, one in each state, and every
// filter shows exactly its own.
func TestTheSourceListFiltersByState(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	// One distinct first letter each: aSourceInTheQueue builds the id from it.
	waiting := aSourceInTheQueue(t, a, "waiting")
	sending := aSourceInTheQueue(t, a, "going")
	suspended := aSourceInTheQueue(t, a, "off")
	refused := aSourceInTheQueue(t, a, "refused")

	apost(t, a, "/sources/"+sending+"/approve", url.Values{}, cookie)
	apost(t, a, "/sources/"+suspended+"/approve", url.Values{}, cookie)
	apost(t, a, "/sources/"+suspended+"/suspend",
		url.Values{"note": {"a complaint"}}, cookie)
	apost(t, a, "/sources/"+refused+"/refuse",
		url.Values{"note": {"not a real company"}}, cookie)

	all := []string{waiting, sending, suspended, refused}
	for state, want := range map[string]string{
		"waiting":   waiting,
		"sending":   sending,
		"suspended": suspended,
		"refused":   refused,
	} {
		page := aget(t, a, "/sources?state="+state, cookie)
		whole(t, "/sources?state="+state, page)

		if !strings.Contains(page.body, want) {
			t.Errorf("?state=%s does not show %s:\n%s", state, want, page.body)
		}
		for _, other := range all {
			if other != want && strings.Contains(page.body, other) {
				t.Errorf("?state=%s also shows %s", state, other)
			}
		}
	}

	// No filter is still every source, so the link back to "All" means it.
	everything := aget(t, a, "/sources", cookie)
	for _, id := range all {
		if !strings.Contains(everything.body, id) {
			t.Errorf("/sources with no filter does not show %s", id)
		}
	}
}

// A state nobody's link produces shows everything and says so, rather than an
// empty page that reads as "there are none in that state".
func TestAnUnknownStateSaysSoRatherThanShowingNothing(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	page := aget(t, a, "/sources?state=pending", cookie)
	whole(t, "/sources?state=pending", page)

	if !strings.Contains(page.body, "no such state") {
		t.Errorf("the page does not say the state was not understood:\n%s", page.body)
	}
	if !strings.Contains(page.body, id) {
		t.Error("an unknown state hid every source instead of showing them")
	}
}

// A listing that hits its cap says so on the page, in words that say what to
// do about it -- this is what NOTIF_ADMIN_LIST_LIMIT and truncate exist for.
// A limit of two and three sources is enough to prove it end to end, from
// config through the use case to the rendered page.
func TestATruncatedListingSaysSoOnThePage(t *testing.T) {
	a := newTestAdminWithLimit(t, 2)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	aSourceInTheQueue(t, a, "acme")
	aSourceInTheQueue(t, a, "billing")
	aSourceInTheQueue(t, a, "care")

	queue := aget(t, a, "/", cookie)
	if !strings.Contains(queue.body, "Not everything is shown") {
		t.Errorf("the queue does not say it was truncated:\n%s", queue.body)
	}

	all := aget(t, a, "/sources", cookie)
	if !strings.Contains(all.body, "Not everything is shown") {
		t.Errorf("/sources does not say it was truncated:\n%s", all.body)
	}
}

// The other half: a page under its cap must not claim there is more than
// there is -- that would be exactly as dishonest as never saying so.
func TestAnUntruncatedListingSaysNothingAboutIt(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	aSourceInTheQueue(t, a, "acme")

	queue := aget(t, a, "/", cookie)
	if strings.Contains(queue.body, "Not everything is shown") {
		t.Errorf("the queue claimed to be truncated when everything fit:\n%s", queue.body)
	}
}

// Every decision writes exactly one audit row, and a refusal that was itself
// refused writes none.
func TestEveryDecisionWritesOneAuditRow(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	apost(t, a, "/sources/"+id+"/refuse", url.Values{"note": {""}}, cookie)
	if n := len(a.audit.entries); n != 0 {
		t.Fatalf("a refusal with no reason wrote %d audit rows, want 0", n)
	}

	apost(t, a, "/sources/"+id+"/approve", url.Values{}, cookie)
	if n := len(a.audit.entries); n != 1 {
		t.Fatalf("approving wrote %d audit rows, want 1", n)
	}
	if got := a.audit.entries[0].Verb; got != usecase.ActSourceApprove {
		t.Errorf("verb = %q", got)
	}
}

// --- people, super_admin only ------------------------------------------------

func idOf(t *testing.T, a *testAdmin, email string) string {
	t.Helper()

	a.users.mu.Lock()
	defer a.users.mu.Unlock()
	u, ok := a.users.rows[email]
	if !ok {
		t.Fatalf("no such user %q", email)
	}
	return u.ID.String()
}

func TestASuperAdminChangesARole(t *testing.T) {
	a := newTestAdmin(t)
	top := signedInAs(t, a, "root@srosha.ir", user.RoleSuperAdmin)
	signedInAdmin(t, a, "member@acme.test")
	id := idOf(t, a, "member@acme.test")

	res := apost(t, a, "/people/"+id+"/role", url.Values{"role": {"admin"}}, top)
	if res.status != http.StatusSeeOther {
		t.Fatalf("role change = %d\n%s", res.status, res.body)
	}

	page := aget(t, a, "/people/"+id, top)
	if !strings.Contains(page.body, "admin") {
		t.Errorf("the person page does not show the new role:\n%s", page.body)
	}
}

// Nobody closes the last door behind themselves.
func TestASuperAdminCannotChangeTheirOwnRole(t *testing.T) {
	a := newTestAdmin(t)
	top := signedInAs(t, a, "root@srosha.ir", user.RoleSuperAdmin)
	id := idOf(t, a, "root@srosha.ir")

	res := apost(t, a, "/people/"+id+"/role", url.Values{"role": {"customer"}}, top)
	if res.status == http.StatusSeeOther {
		t.Error("a super_admin was allowed to change their own role")
	}

	page := aget(t, a, "/people/"+id, top)
	if !strings.Contains(page.body, "super_admin") {
		t.Error("a super_admin demoted their own account")
	}
}

// --- what a source sends as -------------------------------------------------

// An operator sees a source's registered identities on its page -- what it is
// configured to send as, so approving it is not approving blind.
func TestAnOperatorSeesASourcesSenders(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	sender, err := credential.New(
		shared.ID("01K0SNDADMIN000000000001"), id, shared.ChannelTelegram,
		"our-support-bot", false, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("credential.New: %v", err)
	}
	a.credentials.add(id, *sender)

	got := aget(t, a, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	for _, want := range []string{"our-support-bot", "telegram", "In use"} {
		if !strings.Contains(got.body, want) {
			t.Errorf("the source page does not show %q:\n%s", want, got.body)
		}
	}
}

// A source with nothing registered is the ordinary case, not an error -- it
// sends as srosha, which is what makes a first message work. The page says
// so rather than looking like something is missing.
func TestASourceWithNoSendersShowsTheEmptyState(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	got := aget(t, a, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	body := strings.ToLower(got.body)
	for _, want := range []string{"nothing registered", "sends as", "makes a first message work"} {
		if !strings.Contains(body, want) {
			t.Errorf("the empty state does not say %q:\n%s", want, got.body)
		}
	}
}

// --- where a source sends ----------------------------------------------------

// The most decision-relevant fact on the page: where a source's messages go
// by default. Shown in full -- unlike a delivery's masked recipient at
// /sources/:id/log, this is the customer's own declared configuration, not a
// third party.
func TestTheSourcePageShowsItsDefaultAddresses(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme") // seeds acme@acme.test on email

	got := aget(t, a, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	for _, want := range []string{"acme@acme.test", "email"} {
		if !strings.Contains(got.body, want) {
			t.Errorf("the source page does not show its default address %q:\n%s", want, got.body)
		}
	}
}

// A source with no addresses at all is the ordinary case at registration --
// the page says so rather than looking like something is missing.
func TestASourceWithNoDefaultAddressesShowsTheEmptyState(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceWithNoAddress(t, a, "bare")

	got := aget(t, a, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	body := strings.ToLower(got.body)
	if !strings.Contains(body, "no default addresses configured") {
		t.Errorf("the page does not say there are none:\n%s", got.body)
	}
}

// A source with nowhere to send cannot be approved -- neither a default
// address nor permission for a custom one. The operator sees why BEFORE
// pressing Approve, not after it fails: the same words the domain's own
// guard would return, previewed rather than duplicated on the template.
func TestASourceWithNowhereToSendSaysSoBeforeApproving(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceWithNoAddress(t, a, "unreachable")

	got := aget(t, a, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	if !strings.Contains(got.body, "Only the customer can fix this, by adding an address") {
		t.Errorf("the page does not say who can fix it:\n%s", got.body)
	}
}

// A source that CAN be approved gets no such warning.
func TestASourceThatCanBeApprovedShowsNoWarning(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)
	id := aSourceInTheQueue(t, a, "acme")

	got := aget(t, a, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	if strings.Contains(got.body, "Only the customer can fix this") {
		t.Errorf("a source that can be approved is shown a warning anyway:\n%s", got.body)
	}
}

// NewAdmin writes the admin surface's own cookie, and not the portal's.
//
// The unit test in session_test.go proves sessions reads only the name it was
// given; it cannot prove NewAdmin gives it the right one. Passing the portal's
// name here does fail the suite -- as a dozen "could not sign in" lines that
// never say why. This one says why.
func TestTheAdminSurfaceWritesItsOwnCookie(t *testing.T) {
	a := newTestAdmin(t)

	apost(t, a, "/signin", url.Values{"email": {"ops@srosha.ir"}})
	in := apost(t, a, "/signin/code",
		url.Values{"email": {"ops@srosha.ir"}, "code": {a.mail.lastCode(t)}})

	if in.sessionNamed(web.PortalCookieName) != nil {
		t.Error("the admin surface set the portal's cookie, so a customer's " +
			"session would be presented here rather than never sent")
	}
	if in.sessionNamed(web.AdminCookieName) == nil {
		t.Fatal("the admin surface set no session cookie of its own")
	}
}
