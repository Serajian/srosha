package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/gin-gonic/gin"
)

// whoever answers Whoami with one person, or refuses.
type whoever struct{ u *user.User }

func (w whoever) Request(context.Context, string) error { return nil }

func (w whoever) Verify(context.Context, string, string) (*session.Session, error) {
	return nil, errs.UnauthorizedErr("no")
}

func (w whoever) Whoami(context.Context, shared.ID) (*user.User, error) {
	if w.u == nil {
		return nil, errs.NotFoundErr("session not found").WithErr(session.ErrNotFound)
	}
	return w.u, nil
}

func (w whoever) End(context.Context, shared.ID) error { return nil }

func person(t *testing.T, role user.Role) *user.User {
	t.Helper()

	u, err := user.New(shared.ID("01K0ACCT0000000000000000AB"), "a@acme.test", role,
		time.Now().UTC())
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	return u
}

// behind builds one guarded route and reports what a request carrying a cookie
// gets, and whether the page ran.
func behind(t *testing.T, rule may, u *user.User) (status int, ran bool) {
	t.Helper()

	rec, ran := guarded(t, rule, u)
	return rec.Code, ran
}

// guarded is behind with the whole response, for the tests that need to know
// what happened to the cookie.
func guarded(t *testing.T, rule may, u *user.User) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	var ran bool

	gin.SetMode(gin.TestMode)
	sessions := newSessions(whoever{u: u}, portalCookieName, false)

	engine := gin.New()
	engine.GET("/x", sessions.guard(rule, "/signin"), func(c *gin.Context) {
		ran = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: portalCookieName, Value: "01K0SESS0000000000000000AB"})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec, ran
}

// cleared reports whether the response told the browser to drop the session
// cookie.
func cleared(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == portalCookieName && c.Value == "" {
			return true
		}
	}
	return false
}

// An admin who opens a super_admin page is refused and STAYS SIGNED IN.
//
// Clearing the cookie on every refusal is about not telling a customer that
// the admin listener answers at all -- see may.endsSession. Whoever is asking
// here has already proven they are an operator; signing them out costs them
// their session mid-task and an emailed code to get it back, and hides
// nothing they cannot already see in the nav.
func TestARefusedSuperAdminPageKeepsTheOperatorSignedIn(t *testing.T) {
	rec, ran := guarded(t, superAdmin, person(t, user.RoleAdmin))

	if ran {
		t.Fatal("an admin reached a page guarded for super_admins")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect", rec.Code)
	}
	if cleared(rec) {
		t.Error("an admin was signed out for opening a page they may not open")
	}
}

// The other half, and the one that must not change: under the operator rule a
// refusal still ends the session, because the caller may be a customer.
func TestARefusedOperatorPageStillEndsTheSession(t *testing.T) {
	rec, _ := guarded(t, operator, person(t, user.RoleCustomer))
	if !cleared(rec) {
		t.Error("a customer refused by the operator rule kept their cookie")
	}
}

// A session the core will not answer for is cleared under every rule: there is
// nothing left to keep.
func TestAStaleSessionIsClearedUnderEveryRule(t *testing.T) {
	for name, rule := range map[string]may{
		"anybody": anybody, "operator": operator, "superAdmin": superAdmin,
	} {
		t.Run(name, func(t *testing.T) {
			rec, ran := guarded(t, rule, nil)
			if ran {
				t.Fatal("a page ran for a session the core does not know")
			}
			if !cleared(rec) {
				t.Error("a stale cookie was left in the browser")
			}
		})
	}
}

// THE boundary between a customer and the admin surface.
//
// A cookie is not scoped by port, so a customer holding a perfectly valid
// session reaches the admin listener already carrying it. This is the only
// thing that stops them, and it is written before that surface exists so the
// line cannot be left out when it does.
func TestOperatorPagesRefuseACustomer(t *testing.T) {
	status, ran := behind(t, operator, person(t, user.RoleCustomer))

	if ran {
		t.Fatal("a customer reached a page guarded for operators")
	}
	if status != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect to sign in", status)
	}
}

func TestOperatorPagesLetOperatorsThrough(t *testing.T) {
	for _, role := range []user.Role{user.RoleAdmin, user.RoleSuperAdmin} {
		t.Run(role.String(), func(t *testing.T) {
			status, ran := behind(t, operator, person(t, role))

			if !ran {
				t.Fatalf("%s was refused a page guarded for operators", role)
			}
			if status != http.StatusOK {
				t.Errorf("status = %d", status)
			}
		})
	}
}

// The portal's rule is the other one: signing in is the whole of it.
func TestTheCustomerSurfaceAsksOnlyForASession(t *testing.T) {
	if _, ran := behind(t, anybody, person(t, user.RoleCustomer)); !ran {
		t.Error("a signed-in customer was refused their own portal")
	}
	if _, ran := behind(t, anybody, nil); ran {
		t.Error("a page ran for somebody with no session at all")
	}
}

// A refusal says the same thing however it happened. Telling a stale cookie
// from a wrong role would tell a customer that the admin pages are there.
func TestEveryRefusalLooksTheSame(t *testing.T) {
	noSession, _ := behind(t, operator, nil)
	wrongRole, _ := behind(t, operator, person(t, user.RoleCustomer))

	if noSession != wrongRole {
		t.Errorf("no session = %d, wrong role = %d -- they must not differ",
			noSession, wrongRole)
	}
}

// A session cookie belongs to one surface. The admin surface reads its own
// name and nothing else, so a customer's portal session is not refused there:
// it is never presented.
//
// This is what replaced the loopback guard. If it goes green while the code is
// wrong, the panel is on the internet with one boolean in front of it.
func TestOneSurfaceDoesNotReadTheOthersCookie(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reads   string
		carries string
	}{
		{"admin ignores a portal cookie", adminCookieName, portalCookieName},
		{"portal ignores an admin cookie", portalCookieName, adminCookieName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			s := newSessions(whoever{u: person(t, user.RoleAdmin)}, tc.reads, false)

			var got shared.ID
			var ok bool
			engine := gin.New()
			engine.GET("/x", func(c *gin.Context) { got, ok = s.ID(c) })

			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.AddCookie(&http.Cookie{
				Name: tc.carries, Value: "01K0SESS0000000000000000AB",
			})
			engine.ServeHTTP(httptest.NewRecorder(), req)

			if ok {
				t.Errorf("a %s cookie was read as a session by the %s surface: %q",
					tc.carries, tc.reads, got)
			}
		})
	}
}

// The cookie must stay host-only, and host-only means no Domain attribute.
//
// Setting Domain=srosha.ir would send a customer's session to the admin host
// and undo the whole separation, with nothing failing. So the absence is
// asserted rather than assumed.
func TestTheSessionCookieIsHostOnly(t *testing.T) {
	for _, name := range []string{portalCookieName, adminCookieName} {
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			s := newSessions(whoever{}, name, false)

			engine := gin.New()
			engine.GET("/x", func(c *gin.Context) {
				s.begin(c, &session.Session{
					ID:        shared.ID("01K0SESS0000000000000000AB"),
					ExpiresAt: time.Now().Add(time.Hour),
				})
			})

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

			for _, c := range rec.Result().Cookies() {
				if c.Name == name && c.Domain != "" {
					t.Fatalf("cookie %q carries Domain=%q, so it is sent to every "+
						"subdomain -- the two surfaces are no longer separated",
						name, c.Domain)
				}
			}
		})
	}
}
