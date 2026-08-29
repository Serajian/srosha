package web

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// signInHandler is getting in: the address, the code, and the session that
// comes out of them.
type signInHandler struct {
	signIn   SignIn
	sessions *sessions
	log      *slog.Logger
}

// Each page carries its own data. One struct shared by every page would have
// fields that mean nothing on most of them, and a template quietly rendering an
// empty one is how a page ends up missing something nobody noticed.
type (
	signInPage struct {
		chrome
		Email, Problem string
	}
	codePage struct {
		chrome
		Email, Problem string
	}
)

func (h *signInHandler) show(c *gin.Context) {
	c.HTML(http.StatusOK, pageSignIn, signInPage{})
}

// request sends a code and answers the same way for every address.
//
// The core has already turned an unknown address into a customer, sent nothing
// to somebody who may not sign in, and told this handler none of it. So there
// is one answer here, and the only thing that changes it is the request limit
// -- which is about whoever is asking, not about whose address it is.
func (h *signInHandler) request(c *gin.Context) {
	email := formValue(h.log, c, fieldEmail)

	if err := h.signIn.Request(c.Request.Context(), email); err != nil {
		h.log.WarnContext(c.Request.Context(), "sign-in code refused", "error", err)
		c.HTML(http.StatusOK, pageSignIn, signInPage{Email: email, Problem: message(err)})
		return
	}

	// The address travels in the query so a refresh of the code page does not
	// need the form again. It is not a secret: whoever is looking at this
	// screen typed it.
	c.Redirect(http.StatusSeeOther, pathCode+"?"+fieldEmail+"="+urlValue(email))
}

func (h *signInHandler) showCode(c *gin.Context) {
	c.HTML(http.StatusOK, pageCode, codePage{Email: c.Query(fieldEmail)})
}

// verify checks the attempt and begins a session if it was right.
//
// Every failure renders the same sentence. Saying which part was wrong -- the
// address, the code, the timing -- tells whoever is guessing how close they
// got.
func (h *signInHandler) verify(c *gin.Context) {
	email := formValue(h.log, c, fieldEmail)
	code := formValue(h.log, c, fieldCode)

	sess, err := h.signIn.Verify(c.Request.Context(), email, code)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "sign-in refused", "error", err)
		c.HTML(http.StatusOK, pageCode, codePage{Email: email, Problem: message(err)})
		return
	}

	h.sessions.begin(c, sess)
	c.Redirect(http.StatusSeeOther, pathHome)
}
