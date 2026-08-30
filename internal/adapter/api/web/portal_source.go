package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"

	"github.com/gin-gonic/gin"
)

// SourcePages is what this adapter needs from the core. usecase.Sources
// satisfies it.
type SourcePages interface {
	Register(
		ctx context.Context, actor *user.User, reg usecase.SourceRegistration,
	) (*source.Source, error)
	Mine(ctx context.Context, actor *user.User) ([]source.Source, error)
	One(ctx context.Context, actor *user.User, id string) (*source.Source, error)
	Update(
		ctx context.Context, actor *user.User, id string, set usecase.SourceSettings,
	) (*source.Source, error)
}

type sourceHandler struct {
	sources SourcePages
	log     *slog.Logger
}

type (
	sourceListPage struct {
		chrome
		Sources []source.Source
		Problem string
	}
	sourceNewPage struct {
		chrome
		Name    string
		Problem string
	}
	sourcePage struct {
		chrome
		Source *source.Source
	}
	// sourceEditPage carries the channel list rather than the template naming
	// the seven itself. A channel added to shared and not to a hardcoded list
	// is a channel a customer cannot configure, with nothing to say so.
	sourceEditPage struct {
		chrome
		Source   *source.Source
		Channels []shared.Channel
		Problem  string
	}
)

func (h *sourceHandler) list(c *gin.Context) {
	mine, err := h.sources.Mine(c.Request.Context(), signedInUser(c))
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "could not list sources", "error", err)
		c.HTML(http.StatusOK, pageSources, sourceListPage{chrome: inside, Problem: message(err)})
		return
	}
	c.HTML(http.StatusOK, pageSources, sourceListPage{chrome: inside, Sources: mine})
}

func (h *sourceHandler) showNew(c *gin.Context) {
	c.HTML(http.StatusOK, pageSourceNew, sourceNewPage{chrome: inside})
}

// create registers a source. It is switched off, and the page it redirects to
// says so.
func (h *sourceHandler) create(c *gin.Context) {
	name := formValue(h.log, c, fieldName)

	reg := usecase.SourceRegistration{Name: name, DefaultAddresses: defaultAddresses(c)}

	if _, err := h.sources.Register(c.Request.Context(), signedInUser(c), reg); err != nil {
		h.log.WarnContext(c.Request.Context(), "source registration refused", "error", err)
		c.HTML(
			http.StatusOK,
			pageSourceNew,
			sourceNewPage{chrome: inside, Name: name, Problem: message(err)},
		)
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources)
}

func (h *sourceHandler) show(c *gin.Context) {
	src, err := h.sources.One(c.Request.Context(), signedInUser(c), c.Param("id"))
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageSource, sourcePage{chrome: inside, Source: src})
}

func (h *sourceHandler) showEdit(c *gin.Context) {
	src, err := h.sources.One(c.Request.Context(), signedInUser(c), c.Param("id"))
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageSourceEdit, sourceEditPage{
		chrome: inside, Source: src, Channels: shared.AllChannels(),
	})
}

// update writes the three things the customer owns.
//
// The form has no field for anything else, and usecase.SourceSettings has no
// member for it either -- so a posted max_priority is read by nobody rather
// than filtered by somebody.
func (h *sourceHandler) update(c *gin.Context) {
	id := c.Param("id")

	set := usecase.SourceSettings{
		Name:             formValue(h.log, c, fieldName),
		Description:      formValue(h.log, c, fieldDescription),
		DefaultAddresses: defaultAddresses(c),
	}

	if _, err := h.sources.Update(c.Request.Context(), signedInUser(c), id, set); err != nil {
		h.log.WarnContext(c.Request.Context(), "source not changed", "error", err)
		h.editWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id)
}

// editWith renders the form again with something gone wrong on it, over the
// source as it still is rather than as it was asked to be.
func (h *sourceHandler) editWith(c *gin.Context, id, problem string) {
	src, err := h.sources.One(c.Request.Context(), signedInUser(c), id)
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageSourceEdit, sourceEditPage{
		chrome: inside, Source: src, Channels: shared.AllChannels(), Problem: problem,
	})
}

// defaultAddresses reads the repeated channel/address pairs the form posts.
//
// A pair with either half missing is dropped rather than half-stored. A channel
// the service does not have is left alone here and refused by the domain, which
// is the only place that knows the list.
func defaultAddresses(c *gin.Context) map[shared.Channel]string {
	channels := c.Request.PostForm[fieldChannel]
	addresses := c.Request.PostForm[fieldAddress]

	out := map[shared.Channel]string{}
	for i := range channels {
		if i >= len(addresses) {
			break
		}
		channel := strings.TrimSpace(channels[i])
		address := strings.TrimSpace(addresses[i])
		if channel == "" || address == "" {
			continue
		}
		out[shared.Channel(channel)] = address
	}
	return out
}

// notFound is the one answer for a source that is not there and for a source
// somebody else owns. Two answers would let anybody test ids.
func notFound(c *gin.Context) {
	c.String(http.StatusNotFound, "no such source")
}
