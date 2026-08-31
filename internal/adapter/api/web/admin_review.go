package web

import (
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/gin-gonic/gin"
)

// reviewHandler is the operator's ordinary work: the queue, one source, and
// the four decisions that move it -- approve, refuse, suspend, restore. Its
// message log is here too, because diagnosing a customer's complaint is the
// same audience and the same page's business.
type reviewHandler struct {
	ops Operators
	log *slog.Logger
}

type (
	queuePage struct {
		adminChrome
		Sources []source.Source
		Problem string
	}
	// adminSourcesPage carries Selected so the page can say which state it is
	// showing and mark the link that is on -- a filtered list that looks
	// identical to the whole list is a list somebody misreads.
	adminSourcesPage struct {
		adminChrome
		Sources  []source.Source
		Selected string
		Problem  string
	}
	// adminSourcePage carries Senders, its owner's own registered identities,
	// so an operator deciding a source is not deciding blind. Never a secret:
	// credential.Credential keeps its own unexported and gives no way to read
	// it back -- this is what an owner is configured to send as, nothing this
	// type could show even by mistake.
	adminSourcePage struct {
		adminChrome
		Source  *source.Source
		Senders []credential.Credential
		Problem string
	}
	// adminLogPage carries Deliveries only for the one message Selected names --
	// fetching every message's deliveries up front would be a fan-out nobody
	// asked for, on a screen a person reads one message at a time.
	adminLogPage struct {
		adminChrome
		SourceID   string
		Messages   []usecase.OperatorMessage
		Selected   string
		Deliveries []usecase.OperatorDelivery
		Problem    string
	}
)

func (h *reviewHandler) queue(c *gin.Context) {
	actor := signedInUser(c)

	list, err := h.ops.Queue(c.Request.Context(), actor)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "could not read the queue", "error", err)
		c.HTML(http.StatusOK, pageQueue,
			queuePage{adminChrome: chromeFor(actor), Problem: message(err)})
		return
	}
	c.HTML(http.StatusOK, pageQueue, queuePage{adminChrome: chromeFor(actor), Sources: list})
}

// list is every source, narrowed by the `state` query string when there is
// one.
//
// Filtered HERE and not in SQL, which is what queries/source.sql's own comment
// on ListAllSources says: an operator flips between "waiting" and "all" on the
// same screen, and a round trip per flip buys nothing on a set one operator
// reads by eye. When that set is large enough for this to be wrong, the filter
// moves into the statement and this function is what points at it.
func (h *reviewHandler) list(c *gin.Context) {
	actor := signedInUser(c)

	all, err := h.ops.AllSources(c.Request.Context(), actor)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "could not list sources", "error", err)
		c.HTML(http.StatusOK, pageSources,
			adminSourcesPage{adminChrome: chromeFor(actor), Problem: message(err)})
		return
	}

	state := c.Query(fieldState)
	shown, problem := inState(all, state)

	c.HTML(http.StatusOK, pageSources, adminSourcesPage{
		adminChrome: chromeFor(actor), Sources: shown, Selected: state, Problem: problem,
	})
}

// inState keeps the sources in one state, and says so when it does not
// recognize the one it was asked for.
//
// An unknown value shows EVERYTHING with a problem on the page rather than
// nothing in silence: an empty list reads as "there are none in that state",
// which is a different and wrong answer to a question that was never
// understood. Nothing on the page produces such a value -- it means somebody
// typed the query string by hand.
func inState(all []source.Source, state string) ([]source.Source, string) {
	if state == "" {
		return all, ""
	}

	var match func(*source.Source) bool
	switch state {
	case stateSending:
		match = func(s *source.Source) bool { return s.IsActive }
	case stateWaiting:
		match = func(s *source.Source) bool { return !s.IsReviewed() }
	case stateSuspended:
		// Reviewed, approved once, and switched off since.
		match = func(s *source.Source) bool {
			return !s.IsActive && s.IsReviewed() && s.IsApproved()
		}
	case stateRefused:
		// Reviewed and never approved: turned away at the door.
		match = func(s *source.Source) bool {
			return !s.IsActive && s.IsReviewed() && !s.IsApproved()
		}
	default:
		return all, "no such state: " + state
	}

	out := make([]source.Source, 0, len(all))
	for i := range all {
		if match(&all[i]) {
			out = append(out, all[i])
		}
	}
	return out, ""
}

func (h *reviewHandler) show(c *gin.Context) {
	actor := signedInUser(c)

	src, ok := h.readSource(c, c.Param("id"))
	if !ok {
		return
	}
	c.HTML(http.StatusOK, pageSource, adminSourcePage{
		adminChrome: chromeFor(actor), Source: src, Senders: h.senders(c, src.ID),
		Problem: cannotBeLetOut(src),
	})
}

// cannotBeLetOut says why the button this page is about to offer -- Approve
// for a source still in the queue, Restore for one switched off -- would
// fail, so an operator sees the reason before pressing it rather than after.
//
// Asks Source.IsReachable directly rather than calling Approve or Restore to
// find out: those exist to MOVE a source, and using either as a predicate
// would mean any future side effect they grow -- a metric, a log line, a
// call to something -- fires on every page render instead of on a real
// decision.
//
// Restore has a second guard, IsReviewed, that this does not check -- but
// this handler only ever reaches Restore's branch (below, in the template)
// once IsReviewed is already true, so reachability is the only way either
// button can fail from here. If Restore ever grows a guard IsReachable does
// not cover, this stops being true and has to be revisited.
func cannotBeLetOut(src *source.Source) string {
	if src.IsActive || src.IsReachable() {
		return ""
	}
	return "this source has nowhere to send: no default address is set for any " +
		"channel, and custom addresses are not allowed. Only the customer can " +
		"fix this, by adding an address."
}

// approve lets a source send. No reason needed -- the source works, which is
// the whole message.
func (h *reviewHandler) approve(c *gin.Context) {
	id := c.Param("id")

	if err := h.ops.Approve(c.Request.Context(), signedInUser(c), id); err != nil {
		h.log.WarnContext(c.Request.Context(), "source not approved", "error", err)
		h.showWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id)
}

// refuse turns a source away at the door.
//
// The domain refuses this on its own when the reason is empty, or when the
// source was already approved -- the second case is what tells an operator to
// suspend instead, and message(err) is what carries that sentence onto the
// page: source.Refuse's own error text says exactly this.
func (h *reviewHandler) refuse(c *gin.Context) {
	id := c.Param("id")
	note := formValue(h.log, c, fieldNote)

	if err := h.ops.Refuse(c.Request.Context(), signedInUser(c), id, note); err != nil {
		h.log.WarnContext(c.Request.Context(), "source not refused", "error", err)
		h.showWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id)
}

// suspend stops a source that already got through. Its reason is optional --
// the customer sees only that the source was suspended, on their own page,
// not why.
func (h *reviewHandler) suspend(c *gin.Context) {
	id := c.Param("id")
	note := formValue(h.log, c, fieldNote)

	if err := h.ops.Suspend(c.Request.Context(), signedInUser(c), id, note); err != nil {
		h.log.WarnContext(c.Request.Context(), "source not suspended", "error", err)
		h.showWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id)
}

// restore is the way back from a suspension or a refusal.
func (h *reviewHandler) restore(c *gin.Context) {
	id := c.Param("id")

	if err := h.ops.Restore(c.Request.Context(), signedInUser(c), id); err != nil {
		h.log.WarnContext(c.Request.Context(), "source not restored", "error", err)
		h.showWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id)
}

// messages is a source's own log, newest first, metadata only. A `message`
// query string opens one to its deliveries -- a query parameter rather than a
// route of its own, because it is the same page asking a follow-up question,
// not a different screen.
func (h *reviewHandler) messages(c *gin.Context) {
	id := c.Param("id")
	actor := signedInUser(c)

	// The source is read FIRST, and its 404 is the page's own. Messages
	// answers an empty list for a source that does not exist -- there are no
	// rows for it, which is true and useless -- so without this an id with a
	// typo in it rendered a perfectly good log page for nothing, while
	// /sources/:id one click away answered 404 for the same id.
	if _, ok := h.readSource(c, id); !ok {
		return
	}

	msgs, err := h.ops.Messages(c.Request.Context(), actor, id)
	if err != nil {
		h.refuseRead(c, "messages", id, err)
		return
	}

	page := adminLogPage{adminChrome: chromeFor(actor), SourceID: id, Messages: msgs}

	if selected := c.Query(fieldMessage); selected != "" {
		deliveries, err := h.ops.Deliveries(c.Request.Context(), actor, id, selected)
		if err != nil {
			h.log.WarnContext(c.Request.Context(), "could not read deliveries", "error", err)
			page.Selected = selected
			page.Problem = message(err)
		} else {
			page.Selected = selected
			page.Deliveries = deliveries
		}
	}

	c.HTML(http.StatusOK, pageAdminLog, page)
}

// readSource is the one read every route above needs before it can do
// anything else, and the one answer for a source that is not there: writing
// straight to c, so every caller can just check ok.
//
// A source that is not there and a database that will not answer are two
// different answers. Both used to be 404, which tells an operator their id was
// wrong when the truth is that nothing is working -- and leaves no line to
// find afterwards, because a 404 is not worth logging and an outage is.
func (h *reviewHandler) readSource(c *gin.Context, id string) (*source.Source, bool) {
	src, err := h.ops.Source(c.Request.Context(), signedInUser(c), id)
	if err != nil {
		h.refuseRead(c, "source", id, err)
		return nil, false
	}
	return src, true
}

// refuseRead is the one answer for a read that did not come back: 404 when the
// thing is genuinely not there, 500 and a log line when anything else went
// wrong.
func (h *reviewHandler) refuseRead(c *gin.Context, what, id string, err error) {
	if errs.IsType(err, errs.ErrNotFound) {
		notFound(c)
		return
	}
	h.log.ErrorContext(c.Request.Context(), "could not read for an operator page",
		"what", what, "id", id, "error", err)
	c.String(http.StatusInternalServerError, "something went wrong")
}

// showWith renders the source page again with something gone wrong on it,
// over the source as it still is rather than as the decision tried to make it.
func (h *reviewHandler) showWith(c *gin.Context, id, problem string) {
	src, ok := h.readSource(c, id)
	if !ok {
		return
	}
	c.HTML(http.StatusOK, pageSource, adminSourcePage{
		adminChrome: chromeFor(signedInUser(c)), Source: src, Senders: h.senders(c, src.ID),
		Problem: problem,
	})
}

// senders reads what a source is configured to send as. A failure here does
// not stop the source page from rendering -- it is a secondary fact about a
// source that already loaded, the same way the portal's own callback page
// treats a missing callback as ordinary rather than fatal.
func (h *reviewHandler) senders(c *gin.Context, sourceID string) []credential.Credential {
	senders, err := h.ops.Senders(c.Request.Context(), signedInUser(c), sourceID)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "could not read senders", "error", err)
		return nil
	}
	return senders
}
