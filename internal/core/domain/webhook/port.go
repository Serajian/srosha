package webhook

import "context"

type Repository interface {
	Create(ctx context.Context, w *Webhook) error
	ReadBySourceID(ctx context.Context, sourceID string) (*Webhook, error)
	Update(ctx context.Context, w *Webhook) error
}

// Notifier delivers one batch to a source's callback. It is called once per
// message and never retried: the query API is the reliable way to learn an
// outcome, and this is the convenience on top.
type Notifier interface {
	Notify(ctx context.Context, w *Webhook, b Batch) error
}
