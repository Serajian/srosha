// Package nats carries events between the two binaries: the gateway publishes
// one per delivery, the dispatcher consumes them.
//
// The streams, their subjects and their dedup windows are this service's own
// vocabulary and are built here. internal/infra/messagequeue knows only how to
// hold a connection open and does not know what srosha puts on it.
package nats

import (
	"fmt"
	"strings"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Subjects builds the subjects of one stream, and the wildcard that stream is
// captured by. Both come from one root, so the two can never disagree about
// what belongs to it -- which is exactly what a root written out in two places
// would eventually do.
//
// A second stream is a second Subjects with its own root and its own For
// method. Everything general lives here; only ForDispatch knows what a dispatch
// event is.
type Subjects struct{ root string }

// NewSubjects refuses a root that would not survive being a subject token.
//
// A dot would split into two tokens, so the wildcard would capture more than
// the root was meant to name. A wildcard would make the root itself match other
// people's traffic. Neither fails loudly at publish time -- they quietly change
// what the stream holds -- so they are refused here.
func NewSubjects(root string) (Subjects, error) {
	switch {
	case root == "":
		return Subjects{}, errs.InternalErr("subject root is empty")

	case strings.ContainsAny(root, tokenSeparator+tailWildcard+tokenWildcard),
		strings.ContainsAny(root, " \t"):
		return Subjects{}, errs.InternalErr("subject root is not a single token").
			WithStr(fmt.Sprintf("root %q", root))
	}
	return Subjects{root: root}, nil
}

func (s Subjects) Root() string { return s.root }

// IsZero reports a Subjects nobody built. The zero value would produce ".>",
// which captures every subject on the broker -- including other services'.
func (s Subjects) IsZero() bool { return s.root == "" }

// Wildcard is what the stream captures: the root and everything under it.
func (s Subjects) Wildcard() string {
	return s.join(tailWildcard)
}

// join is the general half: a subject is the root and its tokens, in order.
func (s Subjects) join(tokens ...string) string {
	return s.root + tokenSeparator + strings.Join(tokens, tokenSeparator)
}

// ForDispatch is where one delivery is published:
//
//	<root>.<channel>.<priority>        e.g. notify.email.normal
//
// Both tokens are in it because a subject is what a consumer filters on, and
// these are the two ways this work is ever going to be split:
//
//	notify.email.*       one pool for smtp, another for telegram
//	notify.*.critical    a separate queue for what cannot wait
//
// Channel comes first because that is the likelier split: the providers have
// wildly different rate limits, and the priority ordering is the same whichever
// of them is sending.
//
// Lower case, because the rest of the subject is and because Channel already
// is. Priority renders itself as NORMAL for storage, which is a different
// audience.
func (s Subjects) ForDispatch(e shared.DispatchEvent) (string, error) {
	if s.IsZero() {
		return "", errs.InternalErr("subjects were never built")
	}

	// An unknown channel or priority would build a subject nothing listens on:
	// the event would be accepted by the broker and read by nobody, which is
	// the worst of both -- no error, and no delivery.
	if !e.Channel.Valid() {
		return "", errs.InternalErr("event has an unknown channel").
			WithErr(shared.ErrUnknownChannel).
			WithStr(fmt.Sprintf("delivery %q, channel %q", e.DeliveryID, e.Channel))
	}
	if !e.Priority.Valid() {
		return "", errs.InternalErr("event has an unknown priority").
			WithStr(fmt.Sprintf("delivery %q, priority %d", e.DeliveryID, e.Priority))
	}

	return s.join(e.Channel.String(), strings.ToLower(e.Priority.String())), nil
}
