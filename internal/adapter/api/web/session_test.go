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
func behind(t *testing.T, may may, u *user.User) (status int, ran bool) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	sessions := newSessions(whoever{u: u}, false)

	engine := gin.New()
	engine.GET("/x", sessions.guard(may, "/signin"), func(c *gin.Context) {
		ran = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "01K0SESS0000000000000000AB"})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, ran
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
