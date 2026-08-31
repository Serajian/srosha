package web

import (
	"net/http"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/gin-gonic/gin"
)

// sessions is the cookie, and the question of who is behind it. Nothing else in
// this adapter reads or writes one.
//
// Both surfaces get their own, and they share the cookie because a cookie is
// not scoped by port and could not be separated anyway. What separates them is
// the role a surface's guard demands -- see guard.
type sessions struct {
	signIn SignIn
	secure bool
}

func newSessions(signIn SignIn, secure bool) *sessions {
	return &sessions{signIn: signIn, secure: secure}
}

// begin puts the session in a cookie.
//
// http.SetCookie rather than gin's own: gin's takes a max-age and offers no
// SameSite on the call, and this cookie's expiry has to be the session's own
// deadline rather than a duration computed beside it.
func (s *sessions) begin(c *gin.Context, sess *session.Session) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.ID.String(),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
}

// clear removes it. The value is emptied as well as expired, because a browser
// that ignores the date still has nothing left to send.
func (s *sessions) clear(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// ID is what the browser presented, if anything.
func (s *sessions) ID(c *gin.Context) (shared.ID, bool) {
	v, err := c.Cookie(sessionCookieName)
	if err != nil || v == "" {
		return "", false
	}
	return shared.ID(v), true
}

// may is who a surface lets in, and what being refused costs.
//
// Two fields rather than a bare predicate, because the two questions have
// different answers on the two rules below and folding them together signed a
// working operator out mid-task.
type may struct {
	allows func(*user.User) bool

	// endsSession says whether a refusal also clears the cookie.
	//
	// True only where the caller might be a STRANGER to this surface. Clearing
	// is what makes every refusal look identical, so nobody can tell a stale
	// cookie from a wrong role -- and a customer arriving at the admin
	// listener with a perfectly good portal session must not be able to tell
	// that these pages exist. That reasoning is entirely about `operator`.
	//
	// It does not carry to `superAdmin`, where whoever is asking has already
	// proven they are an operator: they know the surface exists, they are
	// looking at its nav, and there is nothing left to hide from them. Signing
	// them out there costs a working operator their session in the middle of a
	// job and an emailed code to get it back, and buys nothing.
	endsSession bool
}

// anybody is the portal's rule: signing in is the whole of it.
var anybody = may{allows: func(*user.User) bool { return true }}

// operator is the admin surface's rule, and it is the entire boundary between a
// customer and the admin pages.
//
// It exists because a cookie is not scoped by port: a customer holding a
// perfectly valid session reaches the admin listener already carrying it. The
// role is read from the live row on every request, so taking it away takes
// effect at once -- for the same reason is_active does.
//
// endsSession, because the caller may be a customer who should not learn that
// this listener answers at all.
var operator = may{
	allows:      func(u *user.User) bool { return u.Role.IsOperator() },
	endsSession: true,
}

// superAdmin is the rule for the pages that change who somebody is, and for
// the audit log, whose rows carry customers' addresses.
//
// Read from the live row like operator, and for the same reason: taking the
// role away has to take effect on the next request rather than the next
// sign-in.
//
// No endsSession: an admin who opens /people is an operator in the middle of
// their work, not a stranger. They are sent back to the queue still signed in.
var superAdmin = may{allows: func(u *user.User) bool { return u.Role == user.RoleSuperAdmin }}

// guard is what makes a route need a session, and the right kind of person.
//
// It is middleware on a group rather than a line at the top of each handler, so
// whether a page is behind sign-in is visible where the routes are listed -- and
// a page added to the guarded group cannot forget it.
//
// It asks the core on every request rather than trusting the cookie. A session
// the core will not answer for is always cleared -- there is nothing left to
// keep. A session it answers for and a rule that refuses the answer is the
// other case, and what happens then is the rule's own decision: see
// may.endsSession.
//
// Under `operator` the two are therefore still indistinguishable, which is the
// property TestEveryRefusalLooksTheSame holds.
func (s *sessions) guard(rule may, refusedPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := s.ID(c)
		if !ok {
			c.Redirect(http.StatusSeeOther, refusedPath)
			c.Abort()
			return
		}

		u, err := s.signIn.Whoami(c.Request.Context(), id)
		if err != nil {
			s.clear(c)
			c.Redirect(http.StatusSeeOther, refusedPath)
			c.Abort()
			return
		}
		if !rule.allows(u) {
			if rule.endsSession {
				s.clear(c)
			}
			c.Redirect(http.StatusSeeOther, refusedPath)
			c.Abort()
			return
		}

		c.Set(contextUser, u)
		c.Next()
	}
}

// signedInUser is who the guard let through.
//
// It panics rather than returning a zero value, because reaching it without the
// guard is a routing mistake and not a runtime condition: a page that reads this
// outside a guarded group is a page serving somebody else's data or none.
//
// This is the cost of gin's single handler signature. The guarded handlers used
// to take the person as a parameter, so mounting one unguarded did not compile.
func signedInUser(c *gin.Context) *user.User {
	v, ok := c.Get(contextUser)
	if !ok {
		panic("web: a guarded page was mounted outside a guarded group")
	}

	u, ok := v.(*user.User)
	if !ok {
		panic("web: something other than a user is under the guard's context key")
	}
	return u
}
