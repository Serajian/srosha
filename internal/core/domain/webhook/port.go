package webhook

import "context"

// Repository has no single Update, on purpose. Two very different callers write
// this row -- the dispatcher after every callback, and the source through the
// API -- and one method that saves the whole entity lets each undo the other:
// the source redirects to a new address, a callback already in flight settles,
// and the save puts the old address back.
//
// So each method writes only what its caller meant to change.
type Repository interface {
	Create(ctx context.Context, w *Webhook) error

	// ReadBySourceID returns a nil webhook when the source has registered
	// none, so the caller can tell "never registered" from "could not look".
	ReadBySourceID(ctx context.Context, sourceID string) (*Webhook, error)

	// Redirect writes the new address. It is the source's own write.
	Redirect(ctx context.Context, w *Webhook) error

	RecordSuccess(ctx context.Context, w *Webhook) error

	// RecordFailure counts in storage and returns the new count. Several
	// callbacks for one source settle at once, and a count carried in from
	// memory loses increments -- this is the counter that decides when a dead
	// endpoint stops being called, so losing them means it is switched off far
	// later than configured, or never.
	RecordFailure(ctx context.Context, w *Webhook) (int, error)

	// Deactivate and Activate write the flag alone. A webhook already in the
	// asked-for state is not an error: two callbacks crossing the failure limit
	// together both arrive here, and only one of them changes anything.
	Deactivate(ctx context.Context, w *Webhook) error
	Activate(ctx context.Context, w *Webhook) error
}

// Notifier delivers one batch to a source's callback. It is called once per
// message and never retried: the query API is the reliable way to learn an
// outcome, and this is the convenience on top.
type Notifier interface {
	Notify(ctx context.Context, w *Webhook, b Batch) error
}
