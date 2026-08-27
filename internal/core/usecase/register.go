package usecase

import (
	"context"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Registrar manages a source's callback.
//
// It uses source.Load rather than Admit: none of this sends anything, and
// changing a callback must not cost a message from the sending quota.
type Registrar struct {
	sources  *source.Service
	webhooks *webhook.Service
	secrets  SigningSecrets
}

// SigningSecrets issues the key a source's callbacks are signed with.
//
// Declared here rather than imported, because whoever holds the encryption keys
// satisfies it and this package never learns that anything was encrypted --
// the same split credentials already use.
type SigningSecrets interface {
	// Issue makes a new secret, stores it sealed, and returns the plaintext
	// exactly once. Called again for the same webhook, it replaces the old one.
	Issue(ctx context.Context, sourceID string, id shared.ID) (string, error)
}

func NewRegistrar(
	sources *source.Service, webhooks *webhook.Service, secrets SigningSecrets,
) *Registrar {
	return &Registrar{sources: sources, webhooks: webhooks, secrets: secrets}
}

// Register sets where this source wants delivery outcomes pushed to. One source
// has one callback, so registering again moves the existing one.
//
// The first registration also issues the signing secret and returns it. It is
// returned exactly once, here, and never by any other call -- what is stored is
// sealed. Registering again to change the address returns an empty secret,
// because the existing one still stands: rotating it silently would break every
// receiver that was already verifying.
//
// A source that loses it calls RotateSecret.
func (r *Registrar) Register(
	ctx context.Context, sourceID string, reg webhook.Registration,
) (w *webhook.Webhook, secret string, err error) {
	if _, err := r.sources.Load(ctx, sourceID); err != nil {
		return nil, "", err
	}

	existing, err := r.webhooks.Get(ctx, sourceID)
	if err != nil {
		return nil, "", err
	}

	w, err = r.webhooks.Register(ctx, sourceID, reg)
	if err != nil {
		return nil, "", err
	}
	if existing != nil {
		return w, "", nil
	}

	// A callback with no secret cannot be signed, and the notifier refuses to
	// send one unsigned -- so a webhook that exists without one is a row that
	// will never fire.
	secret, err = r.secrets.Issue(ctx, sourceID, w.ID)
	if err != nil {
		return nil, "", err
	}
	return w, secret, nil
}

// RotateSecret issues a new signing secret and returns it once.
//
// It exists because the secret is shown once and stored sealed: without this, a
// source that lost theirs could never verify another callback. Every receiver
// still using the old one starts failing the moment this returns, which is the
// point -- that is what a rotation is.
func (r *Registrar) RotateSecret(ctx context.Context, sourceID string) (string, error) {
	w, err := r.Get(ctx, sourceID)
	if err != nil {
		return "", err
	}
	return r.secrets.Issue(ctx, sourceID, w.ID)
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

// Activate is that way back.
func (r *Registrar) Activate(ctx context.Context, sourceID string) error {
	w, err := r.Get(ctx, sourceID)
	if err != nil {
		return err
	}
	return r.webhooks.Activate(ctx, w)
}
