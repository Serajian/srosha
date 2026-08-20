package webhook

import "context"

type Repository interface {
	Create(ctx context.Context, w *Webhook) error
	ReadBySourceID(ctx context.Context, sourceID string) (*Webhook, error)
	Update(ctx context.Context, w *Webhook) error
}
