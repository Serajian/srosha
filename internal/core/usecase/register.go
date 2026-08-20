package usecase

import (
	"context"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/pkg/errs"
)

// Registrar manages a source's callback.
//
// It uses source.Load rather than Admit: none of this sends anything, and
// changing a callback must not cost a message from the sending quota.
type Registrar struct {
	sources  *source.Service
	webhooks *webhook.Service
}

func NewRegistrar(sources *source.Service, webhooks *webhook.Service) *Registrar {
	return &Registrar{sources: sources, webhooks: webhooks}
}

// Register sets where this source wants delivery outcomes pushed to. One source
// has one callback, so registering again moves the existing one.
func (r *Registrar) Register(
	ctx context.Context, sourceID string, reg webhook.Registration,
) (*webhook.Webhook, error) {
	if _, err := r.sources.Load(ctx, sourceID); err != nil {
		return nil, err
	}
	return r.webhooks.Register(ctx, sourceID, reg)
}

func (r *Registrar) Get(ctx context.Context, sourceID string) (*webhook.Webhook, error) {
	if _, err := r.sources.Load(ctx, sourceID); err != nil {
		return nil, err
	}

	w, err := r.webhooks.Get(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errs.NotFoundErr("no callback registered").WithErr(webhook.ErrNotFound)
	}
	return w, nil
}

// Deactivate stops the callbacks without forgetting the address, so turning
// them back on does not mean registering again.
func (r *Registrar) Deactivate(ctx context.Context, sourceID string) error {
	w, err := r.Get(ctx, sourceID)
	if err != nil {
		return err
	}
	return r.webhooks.Deactivate(ctx, w)
}
