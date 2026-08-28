package web

import (
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/user"

	"github.com/gin-gonic/gin"
)

// accountHandler is being in: who you are, and leaving.
type accountHandler struct {
	signIn   SignIn
	sessions *sessions
	log      *slog.Logger
}

type accountPage struct{ User *user.User }

// show is everything the portal knows about somebody. It is in the guarded
// group, so the person is read from the context rather than looked up again.
func (h *accountHandler) show(c *gin.Context) {
	c.HTML(http.StatusOK, pageAccount, accountPage{User: signedInUser(c)})
}

// signOut ends the session, and does not care whether it was already gone: the
// browser is signed out either way, which is the whole of what was asked for.
//
// It is deliberately NOT in the guarded group -- somebody whose session already
// expired still gets their cookie cleared.
func (h *accountHandler) signOut(c *gin.Context) {
	if id, ok := h.sessions.ID(c); ok {
		if err := h.signIn.End(c.Request.Context(), id); err != nil {
			h.log.WarnContext(
				c.Request.Context(), "sign-out could not remove the session", "error", err,
			)
		}
	}

	h.sessions.clear(c)
	c.Redirect(http.StatusSeeOther, pathSignIn)
}
