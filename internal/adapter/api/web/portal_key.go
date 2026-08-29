package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/gin-gonic/gin"
)

// KeyPages is what this adapter needs from the core. usecase.Keys satisfies it.
type KeyPages interface {
	Issue(
		ctx context.Context, actor *user.User, sourceID, label string,
	) (string, *source.Key, error)
	List(ctx context.Context, actor *user.User, sourceID string) ([]source.Key, error)
	Revoke(ctx context.Context, actor *user.User, sourceID string, keyID shared.ID) error
}

type keyHandler struct {
	keys KeyPages
	log  *slog.Logger
}

type keysPage struct {
	SourceID string
	Keys     []source.Key
	Problem  string
}

// keyIssuedPage carries the key itself, exactly once. Nothing stores it and
// nothing can render it again.
type keyIssuedPage struct {
	SourceID string
	Key      string
	Label    string
}

func (h *keyHandler) list(c *gin.Context) {
	id := c.Param("id")

	keys, err := h.keys.List(c.Request.Context(), signedInUser(c), id)
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageKeys, keysPage{SourceID: id, Keys: keys})
}

// issue renders the key straight into the response and keeps it nowhere.
//
// There is no redirect afterwards on purpose: a redirect would need somewhere
// to put the key in the meantime -- a session, a flash, a query string -- and
// every one of those outlives the page it was meant for.
func (h *keyHandler) issue(c *gin.Context) {
	id := c.Param("id")
	label := formValue(h.log, c, fieldLabel)

	key, made, err := h.keys.Issue(c.Request.Context(), signedInUser(c), id, label)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "key not issued", "error", err)
		h.listWith(c, id, message(err))
		return
	}

	c.HTML(http.StatusOK, pageKeyIssued, keyIssuedPage{
		SourceID: id, Key: key, Label: made.Label,
	})
}

func (h *keyHandler) revoke(c *gin.Context) {
	id := c.Param("id")

	err := h.keys.Revoke(c.Request.Context(), signedInUser(c), id, shared.ID(c.Param("keyID")))
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "key not revoked", "error", err)
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id+"/keys")
}

// listWith renders the list with something gone wrong on it. A failed issue
// leaves the customer where they were, with the keys they still have.
func (h *keyHandler) listWith(c *gin.Context, sourceID, problem string) {
	keys, err := h.keys.List(c.Request.Context(), signedInUser(c), sourceID)
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageKeys, keysPage{SourceID: sourceID, Keys: keys, Problem: problem})
}
