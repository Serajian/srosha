package web

import (
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/usecase"

	"github.com/gin-gonic/gin"
)

// auditHandler is who did what. One page, one read, no filter -- see
// usecase.Operators.Audit for why.
//
// super_admin only, and the reason is the ActorEmail column rather than
// anything visible on the page: most acts are a customer's own, so this log
// is the roster spelled out one address at a time. See NewAdmin's `top`
// group and usecase.Operators.Audit.
type auditHandler struct {
	ops Operators
	log *slog.Logger
}

type auditPage struct {
	adminChrome
	Entries   []usecase.AuditEntry
	Truncated bool
	Problem   string
}

func (h *auditHandler) show(c *gin.Context) {
	actor := signedInUser(c)

	entries, truncated, err := h.ops.Audit(c.Request.Context(), actor)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "could not read the audit log", "error", err)
		c.HTML(http.StatusOK, pageAudit,
			auditPage{adminChrome: chromeFor(actor), Problem: message(err)})
		return
	}
	c.HTML(http.StatusOK, pageAudit, auditPage{
		adminChrome: chromeFor(actor), Entries: entries, Truncated: truncated,
	})
}
