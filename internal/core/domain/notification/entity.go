// Package notification holds the message a source submitted: what to say, at
// what priority, and until when. Where it goes is the delivery aggregate.
package notification

import (
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Notification is the message, and nothing else. It has no status: each delivery
// carries its own, and a summary over them is a query. Only metadata is hidden,
// so the caller's map cannot be changed after validation.
type Notification struct {
	ID             shared.ID
	SourceID       string
	IdempotencyKey string

	// A snapshot, so a callback sent later describes the source as it was.
	SourceName string

	Title string
	Body  string

	// Both kept, so the gateway can report a downgrade instead of hiding it.
	RequestedPriority shared.Priority
	EffectivePriority shared.Priority

	// Nil means no deadline.
	ExpireAt *time.Time

	CreatedAt time.Time

	metadata map[string]string
}

// New validates the request and returns the message. We supply the id and the
// clock; the service supplies the origin it resolved from the source.
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

		// Clamp, never reject: a ceiling is not the caller's mistake.
		EffectivePriority: req.Priority.Clamp(org.MaxPriority),

		ExpireAt:  req.ExpireAt,
		CreatedAt: now,
		metadata:  copyMetadata(req.Metadata),
	}, nil
}

// Restore rebuilds from storage WITHOUT validation: a row valid when written
// must stay loadable when a rule tightens. Repository only.
func Restore(base Notification, metadata map[string]string) *Notification {
	base.metadata = copyMetadata(metadata)
	return &base
}

// Metadata returns a copy.
func (n *Notification) Metadata() map[string]string { return copyMetadata(n.metadata) }

// Derived, so it can never disagree with the two priorities.
func (n *Notification) WasDowngraded() bool {
	return n.EffectivePriority != n.RequestedPriority
}

// IsExpired takes the clock: the domain reads no ambient state.
func (n *Notification) IsExpired(now time.Time) bool {
	return n.ExpireAt != nil && !n.ExpireAt.After(now)
}

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
