package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/user"
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

	// Switching one off is not deleting it, and there is no delete: after an
	// incident the first question is when an identity was withdrawn, and a row
	// that is gone answers nothing.
	Deactivate(
		ctx context.Context, sourceID string, id shared.ID,
	) (*credential.Credential, error)
	Activate(
		ctx context.Context, sourceID string, id shared.ID,
	) (*credential.Credential, error)
}

// TrialPages sends one real message through an identity the source registered,
// so a customer finds out whether it works now rather than when a notification
// does not arrive. usecase.Trials satisfies it.
//
// Its own interface rather than a method on SenderPages: two binaries build the
// use case behind SenderPages and only the console can send.
type TrialPages interface {
	Run(
		ctx context.Context, actor *user.User, sourceID string, credentialID shared.ID,
	) (string, error)
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
	trials    TrialPages
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

		// Result is the good news, and Problem the bad. Two fields rather than
		// one with a flag beside it: the page styles them differently, and a
		// single field would need the template to ask which kind it is.
		Result string
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
	h.renderSenders(c, id, problem, "")
}

func (h *identityHandler) listSendersOK(c *gin.Context, id, result string) {
	h.renderSenders(c, id, "", result)
}

func (h *identityHandler) renderSenders(c *gin.Context, id, problem, result string) {
	senders, err := h.senders.List(c.Request.Context(), id)
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageSenders, sendersPage{
		chrome: inside, SourceID: id, Senders: senders, Problem: problem, Result: result,
	})
}

// testSender really sends. It renders the list again with the answer rather
// than redirecting, for the same reason keyHandler.issue does: a redirect needs
// somewhere to keep the message in the meantime, and every such place outlives
// the page it was meant for.
//
// The provider's own words are what reaches the screen. "401 Unauthorized" is
// something a customer can act on; "test failed" is not.
func (h *identityHandler) testSender(c *gin.Context) {
	id, ok := h.mine(c)
	if !ok {
		return
	}

	providerID, err := h.trials.Run(
		c.Request.Context(), signedInUser(c), id, shared.ID(c.Param("senderID")),
	)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "trial refused", "error", err)
		h.listSendersWith(c, id, message(err))
		return
	}
	h.listSendersOK(c, id, "Sent. The provider called it "+providerID+".")
}

// switchSender turns one identity off or back on. Which of the two is decided
// by the route rather than by a posted value, so a form cannot ask for the one
// the page did not offer.
func (h *identityHandler) switchSender(on bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := h.mine(c)
		if !ok {
			return
		}

		move := h.senders.Deactivate
		if on {
			move = h.senders.Activate
		}

		if _, err := move(c.Request.Context(), id, shared.ID(c.Param("senderID"))); err != nil {
			h.log.WarnContext(c.Request.Context(), "sender not switched", "error", err)
			h.listSendersWith(c, id, message(err))
			return
		}
		c.Redirect(http.StatusSeeOther, pathSources+"/"+id+"/senders")
	}
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
