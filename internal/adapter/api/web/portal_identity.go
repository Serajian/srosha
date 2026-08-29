package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"

	"github.com/gin-gonic/gin"
)

// SenderPages is a source's own identities: its bot, its mail account, its
// signing key. usecase.Credentials satisfies it.
type SenderPages interface {
	Register(
		ctx context.Context, sourceID string, reg usecase.CredentialRegistration,
	) (*credential.Credential, error)
	List(ctx context.Context, sourceID string) ([]credential.Credential, error)
}

// CallbackPages is where a source is told what happened. usecase.Registrar
// satisfies it.
type CallbackPages interface {
	Register(
		ctx context.Context, sourceID string, reg webhook.Registration,
	) (*webhook.Webhook, string, error)
	Get(ctx context.Context, sourceID string) (*webhook.Webhook, error)
}

// identityHandler owns both, because both answer the same question from the
// customer's side: what does this source send as, and where is it told what
// happened.
//
// It holds sources as well, and must: usecase.Credentials and usecase.Registrar
// take a source id and check nothing about who is asking. Ownership is checked
// here, first, on every route.
type identityHandler struct {
	senders   SenderPages
	callbacks CallbackPages
	sources   SourcePages
	log       *slog.Logger
}

type (
	sendersPage struct {
		chrome
		SourceID string
		Senders  []credential.Credential
		Problem  string
	}
	callbackPage struct {
		chrome
		SourceID string
		Callback *webhook.Webhook
		Problem  string
	}
	// The secret is exported because html/template reads it, and tagged
	// json:"-" because nothing may ever serialize this page. It exists for the
	// length of one response and is not written down anywhere.
	callbackSecretPage struct {
		chrome
		SourceID string
		Secret   string `json:"-"`
		URL      string
	}
)

// mine is the ownership check every route here runs first. The use cases below
// it take a source id and would happily act on anybody's.
func (h *identityHandler) mine(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if _, err := h.sources.One(c.Request.Context(), signedInUser(c), id); err != nil {
		notFound(c)
		return "", false
	}
	return id, true
}

func (h *identityHandler) showSenders(c *gin.Context) {
	id, ok := h.mine(c)
	if !ok {
		return
	}

	senders, err := h.senders.List(c.Request.Context(), id)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "could not list senders", "error", err)
		c.HTML(
			http.StatusOK,
			pageSenders,
			sendersPage{chrome: inside, SourceID: id, Problem: message(err)},
		)
		return
	}
	c.HTML(http.StatusOK, pageSenders, sendersPage{chrome: inside, SourceID: id, Senders: senders})
}

// addSender registers one of the source's own identities.
//
// A source that registers none is not broken: it sends as srosha, which is the
// default and the reason a first message works at all. This page is for a
// customer who wants their own name on it.
func (h *identityHandler) addSender(c *gin.Context) {
	id, ok := h.mine(c)
	if !ok {
		return
	}

	reg := usecase.CredentialRegistration{
		Channel: shared.Channel(formValue(h.log, c, fieldChannel)),
		Name:    formValue(h.log, c, fieldName),
		Secret:  formValue(h.log, c, fieldSecret),
		Config:  []byte(formValue(h.log, c, fieldConfig)),
	}
	if len(reg.Config) == 0 {
		reg.Config = []byte("{}")
	}

	if _, err := h.senders.Register(c.Request.Context(), id, reg); err != nil {
		h.log.WarnContext(c.Request.Context(), "sender not registered", "error", err)
		h.listSendersWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id+"/senders")
}

func (h *identityHandler) listSendersWith(c *gin.Context, id, problem string) {
	senders, err := h.senders.List(c.Request.Context(), id)
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageSenders, sendersPage{
		chrome: inside, SourceID: id, Senders: senders, Problem: problem,
	})
}

func (h *identityHandler) showCallback(c *gin.Context) {
	id, ok := h.mine(c)
	if !ok {
		return
	}

	// A source with no callback is ordinary, not an error: being told what
	// happened is optional, and the page offers to set one up.
	current, _ := h.callbacks.Get(c.Request.Context(), id)
	c.HTML(
		http.StatusOK,
		pageCallback,
		callbackPage{chrome: inside, SourceID: id, Callback: current},
	)
}

// setCallback registers where this source is told what happened, and hands over
// the signing secret exactly once -- the same rule as a key, for the same
// reason.
func (h *identityHandler) setCallback(c *gin.Context) {
	id, ok := h.mine(c)
	if !ok {
		return
	}

	address := formValue(h.log, c, fieldURL)

	w, secret, err := h.callbacks.Register(
		c.Request.Context(), id, webhook.Registration{CallbackURL: address},
	)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "callback not registered", "error", err)
		c.HTML(
			http.StatusOK,
			pageCallback,
			callbackPage{chrome: inside, SourceID: id, Problem: message(err)},
		)
		return
	}

	c.HTML(http.StatusOK, pageCallbackSecret, callbackSecretPage{
		chrome: inside, SourceID: id, Secret: secret, URL: w.CallbackURL,
	})
}
