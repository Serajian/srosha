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

// may reports whether a person is allowed on a surface at all. The portal
// accepts anybody who can sign in; the admin surface will demand an operator.
type may func(*user.User) bool

// anybody is the portal's rule: signing in is the whole of it.
func anybody(*user.User) bool { return true }

// operator is the admin surface's rule, and it is the entire boundary between a
// customer and the admin pages.
//
// It exists because a cookie is not scoped by port: a customer holding a
// perfectly valid session reaches the admin listener already carrying it. The
// role is read from the live row on every request, so taking it away takes
// effect at once -- for the same reason is_active does.
func operator(u *user.User) bool { return u.Role.IsOperator() }

// guard is what makes a route need a session, and the right kind of person.
//
// It is middleware on a group rather than a line at the top of each handler, so
// whether a page is behind sign-in is visible where the routes are listed -- and
// a page added to the guarded group cannot forget it.
//
// It asks the core on every request rather than trusting the cookie, and every
// refusal ends the same way: the cookie is cleared and the browser is sent to
// sign in. Telling them apart would only tell whoever holds a stale cookie
// which half of it was still good, or tell a customer that the admin pages are
// there at all.
func (s *sessions) guard(may may, signInPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := s.ID(c)
		if !ok {
			c.Redirect(http.StatusSeeOther, signInPath)
			c.Abort()
			return
		}

		u, err := s.signIn.Whoami(c.Request.Context(), id)
		if err != nil || !may(u) {
			s.clear(c)
			c.Redirect(http.StatusSeeOther, signInPath)
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
