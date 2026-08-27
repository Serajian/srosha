package srosha

import (
	"context"
	"time"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
)

// Webhooks is where srosha pushes a delivery's final status.
//
// One per source, which is why nothing here takes an id: the caller is the
// source, and which source that is comes from their key.
//
// The callback is best effort and is never retried past the configured limit,
// so it is a convenience and not the reliable path. Get and List are.
type Webhooks struct {
	client *Client
	api    pb.WebhookServiceClient
}

// Webhook is a callback as the service describes it back.
type Webhook struct {
	ID          string
	CallbackURL string

	// Active says srosha will call it. A dead endpoint is switched off rather
	// than called once per message forever.
	Active bool

	// ConsecutiveFailures is how many callbacks in a row have failed. Any
	// success clears it; reaching the configured limit switches the callback
	// off.
	ConsecutiveFailures int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Register says where to POST outcomes.
//
// It must be https, and it must not be an address inside srosha's own network:
// a callback makes srosha call somewhere the source chose, so without that
// check a source could reach anything srosha can.
//
// **The first registration returns your signing secret, and it is the only
// time anything does.** srosha keeps it encrypted and never hands it back; a
// source that loses it calls RotateSecret. Give it to NewVerifier and check
// every callback with it.
//
// Registering again to change the address returns an empty secret: the one you
// have still stands. Rotating it silently would break every receiver that was
// already verifying.
func (w *Webhooks) Register(
	ctx context.Context, callbackURL string,
) (hook Webhook, secret string, err error) {
	var res *pb.RegisterResponse
	if err := w.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = w.api.Register(ctx, &pb.RegisterRequest{CallbackUrl: callbackURL})
		return err
	}); err != nil {
		return Webhook{}, "", err
	}
	return fromWebhook(res.GetWebhook()), res.GetSecret(), nil
}

// RotateSecret issues a new signing secret and returns it once.
//
// Reach for it when the old one is lost or has leaked. Every receiver still
// checking with it starts failing the moment this returns -- that is what a
// rotation is, so change what verifies before you call it, or accept the gap.
func (w *Webhooks) RotateSecret(ctx context.Context) (string, error) {
	var res *pb.RotateSecretResponse
	if err := w.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = w.api.RotateSecret(ctx, &pb.RotateSecretRequest{})
		return err
	}); err != nil {
		return "", err
	}
	return res.GetSecret(), nil
}

// Get answers what this source's callback is, and how it is doing.
func (w *Webhooks) Get(ctx context.Context) (Webhook, error) {
	var res *pb.WebhookServiceGetResponse
	if err := w.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = w.api.Get(ctx, &pb.WebhookServiceGetRequest{})
		return err
	}); err != nil {
		return Webhook{}, err
	}
	return fromWebhook(res.GetWebhook()), nil
}

// Deactivate stops the callbacks without forgetting the address.
func (w *Webhooks) Deactivate(ctx context.Context) (Webhook, error) {
	var res *pb.DeactivateResponse
	if err := w.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = w.api.Deactivate(ctx, &pb.DeactivateRequest{})
		return err
	}); err != nil {
		return Webhook{}, err
	}
	return fromWebhook(res.GetWebhook()), nil
}

// Activate is the way back, and it clears the failure count: an endpoint
// switched off for failing gets a fresh start rather than one more failure.
func (w *Webhooks) Activate(ctx context.Context) (Webhook, error) {
	var res *pb.ActivateResponse
	if err := w.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = w.api.Activate(ctx, &pb.ActivateRequest{})
		return err
	}); err != nil {
		return Webhook{}, err
	}
	return fromWebhook(res.GetWebhook()), nil
}

func fromWebhook(w *pb.Webhook) Webhook {
	if w == nil {
		return Webhook{}
	}
	return Webhook{
		ID:                  w.GetId(),
		CallbackURL:         w.GetCallbackUrl(),
		Active:              w.GetIsActive(),
		ConsecutiveFailures: int(w.GetConsecutiveFailures()),
		CreatedAt:           fromTimestamp(w.GetCreatedAt()),
		UpdatedAt:           fromTimestamp(w.GetUpdatedAt()),
	}
}
