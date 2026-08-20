// Package notification holds the message a source submitted: what to say, at
// what priority, and until when. Where it goes is the delivery aggregate.
package notification

import (
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Notification is the message, and nothing else.
//
// It has no status: each delivery carries its own, and a summary over them is a
// query, not stored state. Every field is settled at construction; only metadata
// is unexported, so the caller's map cannot be changed after validation.
type Notification struct {
	ID             shared.ID
	SourceID       string
	IdempotencyKey string

	// SourceName is a snapshot, so a callback sent later describes the source as
	// it was when the message was accepted, with no join.
	SourceName string

	Title string
	Body  string

	// Both are kept so the gateway can report a downgrade instead of hiding it.
	RequestedPriority shared.Priority
	EffectivePriority shared.Priority

	// ExpireAt bounds how long delivery is still worth attempting. Nil means no
	// deadline.
	ExpireAt *time.Time

	CreatedAt time.Time

	metadata map[string]string
}

// New validates the request and returns the message. We supply the id and the
// clock, the service supplies the origin it resolved from the source.
func New(id shared.ID, org Origin, req Request, now time.Time) (*Notification, error) {
	// Ours first: never report our own bug as something the caller can fix.
	if id.IsZero() {
		return nil, errs.InternalErr("notification id is missing").
			WithErr(shared.ErrInvalidID)
	}
	if org.ID == "" {
		return nil, errs.InternalErr("source is missing").
			WithErr(ErrMissingSource)
	}
	if now.IsZero() {
		return nil, errs.InternalErr("creation timestamp is missing").
			WithErr(ErrMissingTimestamp)
	}

	// Then the caller's.
	if req.Body == "" {
		return nil, errs.InvalidInputErr("body is required").
			WithErr(ErrEmptyBody)
	}
	if !req.Priority.Valid() {
		return nil, errs.InvalidInputErr("unknown priority").
			WithErr(shared.ErrUnknownPriority).
			WithStr(fmt.Sprintf("got %d", req.Priority))
	}
	if req.ExpireAt != nil && !req.ExpireAt.After(now) {
		return nil, errs.InvalidInputErr("expiry is not in the future").
			WithErr(ErrAlreadyExpired).
			WithStr(fmt.Sprintf("expire_at %s, now %s",
				req.ExpireAt.Format(time.RFC3339), now.Format(time.RFC3339)))
	}

	return &Notification{
		ID:                id,
		SourceID:          org.ID,
		SourceName:        org.Name,
		IdempotencyKey:    req.IdempotencyKey,
		Title:             req.Title,
		Body:              req.Body,
		RequestedPriority: req.Priority,

		// Clamp, never reject: a source's own ceiling must not become a runtime
		// error for its callers.
		EffectivePriority: req.Priority.Clamp(org.MaxPriority),

		ExpireAt:  req.ExpireAt,
		CreatedAt: now,
		metadata:  copyMetadata(req.Metadata),
	}, nil
}

// Restore rebuilds a message from storage WITHOUT validation: a row valid when
// written must stay loadable when a rule tightens. Only the repository calls it.
func Restore(base Notification, metadata map[string]string) *Notification {
	base.metadata = copyMetadata(metadata)
	return &base
}

// Metadata returns a copy. Nil when none was supplied.
func (n *Notification) Metadata() map[string]string { return copyMetadata(n.metadata) }

// WasDowngraded is derived, so it can never disagree with the two priorities.
func (n *Notification) WasDowngraded() bool {
	return n.EffectivePriority != n.RequestedPriority
}

// IsExpired takes the clock, for the same reason New does.
func (n *Notification) IsExpired(now time.Time) bool {
	return n.ExpireAt != nil && !n.ExpireAt.After(now)
}

// copyMetadata detaches a map, so neither side can change it afterwards.
func copyMetadata(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
