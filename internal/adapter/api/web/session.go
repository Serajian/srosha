package web

import (
	"net/http"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
)

// sessions is the cookie, and the question of who is behind it. Nothing else
// in this package reads or writes one.
type sessions struct {
	signIn SignIn
	secure bool
}

// begin puts the session in a cookie.
//
// The cookie carries the session's id and nothing else: everything about who
// this is lives in a row, which is what makes deactivating somebody take effect
// on their next request rather than whenever a token would have expired.
func (s *sessions) begin(w http.ResponseWriter, sess *session.Session) {
	http.SetCookie(w, &http.Cookie{
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
func (s *sessions) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
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

// id is what the browser presented, if anything.
func (s *sessions) id(r *http.Request) (shared.ID, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return shared.ID(c.Value), true
}

// guarded is a page that only exists for somebody signed in. It is handed the
// person, so no handler reads the cookie or asks who this is a second time.
type guarded func(w http.ResponseWriter, r *http.Request, u *user.User)

// guard is what makes a route need a session.
//
// It sits in the route table rather than at the top of each handler, so whether
// a page is behind sign-in is visible where the routes are listed -- and a new
// page cannot be added without deciding.
//
// It asks the core on every request rather than trusting the cookie, and a
// refusal of any kind ends the same way: the cookie is cleared and the browser
// is sent to sign in. Telling the two apart would only tell whoever holds a
// stale cookie which half of it was still good.
func (s *sessions) guard(h guarded) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.id(r)
		if !ok {
			redirect(w, r, pathSignIn)
			return
		}

		u, err := s.signIn.Whoami(r.Context(), id)
		if err != nil {
			s.clear(w)
			redirect(w, r, pathSignIn)
			return
		}
		h(w, r, u)
	}
}
