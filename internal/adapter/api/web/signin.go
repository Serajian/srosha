package web

import (
	"log/slog"
	"net/http"
)

// signInHandler is getting in: the address, the code, and the session that
// comes out of them.
type signInHandler struct {
	signIn   SignIn
	sessions *sessions
	render   *renderer
	log      *slog.Logger
}

// Each page carries its own data. One struct shared by every page would have
// fields that mean nothing on most of them, and a template quietly rendering an
// empty one is how a page ends up missing something nobody noticed.
type (
	signInPage struct{ Email, Problem string }
	codePage   struct{ Email, Problem string }
)

func (h *signInHandler) show(w http.ResponseWriter, r *http.Request) {
	h.render.page(w, r, "signin", signInPage{})
}

// request sends a code and answers the same way for every address.
//
// The core has already turned an unknown address into a customer, sent nothing
// to somebody who may not sign in, and told this handler none of it. So there
// is one answer here, and the only thing that changes it is the request limit
// -- which is about whoever is asking, not about whose address it is.
func (h *signInHandler) request(w http.ResponseWriter, r *http.Request) {
	email := formValue(h.log, r, fieldEmail)

	if err := h.signIn.Request(r.Context(), email); err != nil {
		h.log.WarnContext(r.Context(), "sign-in code refused", "error", err)
		h.render.page(w, r, "signin", signInPage{Email: email, Problem: message(err)})
		return
	}

	// The address travels in the query so a refresh of the code page does not
	// need the form again. It is not a secret: whoever is looking at this
	// screen typed it.
	redirect(w, r, pathCode+"?"+fieldEmail+"="+urlValue(email))
}

func (h *signInHandler) showCode(w http.ResponseWriter, r *http.Request) {
	h.render.page(w, r, "code", codePage{Email: r.URL.Query().Get(fieldEmail)})
}

// verify checks the attempt and begins a session if it was right.
//
// Every failure renders the same sentence. Saying which part was wrong -- the
// address, the code, the timing -- tells whoever is guessing how close they
// got.
func (h *signInHandler) verify(w http.ResponseWriter, r *http.Request) {
	email, code := formValue(h.log, r, fieldEmail), formValue(h.log, r, fieldCode)

	sess, err := h.signIn.Verify(r.Context(), email, code)
	if err != nil {
		h.log.WarnContext(r.Context(), "sign-in refused", "error", err)
		h.render.page(w, r, "code", codePage{Email: email, Problem: message(err)})
		return
	}

	h.sessions.begin(w, sess)
	redirect(w, r, pathHome)
}
