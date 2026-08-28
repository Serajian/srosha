package web

import (
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/user"
)

// accountHandler is being in: who you are, and leaving.
type accountHandler struct {
	signIn   SignIn
	sessions *sessions
	render   *renderer
	log      *slog.Logger
}

type accountPage struct{ User *user.User }

// show is everything the portal knows about somebody. It is behind the guard,
// so the person is handed in rather than looked up again here.
func (h *accountHandler) show(w http.ResponseWriter, r *http.Request, u *user.User) {
	h.render.page(w, r, "account", accountPage{User: u})
}

// signOut ends the session, and does not care whether it was already gone: the
// browser is signed out either way, which is the whole of what was asked for.
//
// It is not behind the guard for that reason -- somebody whose session already
// expired still gets their cookie cleared.
func (h *accountHandler) signOut(w http.ResponseWriter, r *http.Request) {
	if id, ok := h.sessions.id(r); ok {
		if err := h.signIn.End(r.Context(), id); err != nil {
			h.log.WarnContext(r.Context(), "sign-out could not remove the session", "error", err)
		}
	}

	h.sessions.clear(w)
	redirect(w, r, pathSignIn)
}
