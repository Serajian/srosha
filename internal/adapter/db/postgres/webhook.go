package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WebhookRepository implements webhook.Repository.
//
// There is no Save that writes the whole row. The dispatcher and the source's
// own API both write this row, and each statement here touches only the columns
// its caller meant to change -- otherwise a callback still in flight puts the
// old address back over a redirect that already happened.
type WebhookRepository struct{ base }

func NewWebhookRepository(pool *pgxpool.Pool) *WebhookRepository {
	return &WebhookRepository{base{pool: pool}}
}

// Create registers the callback. is_active and consecutive_failures are not
// passed: a new callback is switched on and has failed at nothing, and the
// column defaults say so.
//
// One source has one callback, and the unique constraint is what makes that
// true. The service reads before it writes, so reaching a violation here means
// two registrations arrived at once.
func (r *WebhookRepository) Create(ctx context.Context, w *webhook.Webhook) error {
	err := r.q(ctx).CreateWebhook(ctx, gen.CreateWebhookParams{
		ID:          w.ID.String(),
		SourceID:    w.SourceID,
		CallbackUrl: w.CallbackURL,
		CreatedAt:   w.CreatedAt,
	})
	switch {
	case violates(err, "webhooks_source_id_key"):
		return errs.DuplicateErr("this source already has a callback").WithStr(w.SourceID)
	case violates(err, "webhooks_pkey"):
		return errs.DuplicateErr("webhook already exists").WithStr(w.ID.String())
	case err != nil:
		return failed("create webhook", err)
	}
	return nil
}

// ReadBySourceID answers with a nil webhook when the source registered none.
// That is the port's contract: Register uses it to decide between creating one
// and redirecting the existing one, and an error there would turn an ordinary
// first registration into a failure.
func (r *WebhookRepository) ReadBySourceID(
	ctx context.Context, sourceID string,
) (*webhook.Webhook, error) {
	row, err := r.q(ctx).ReadWebhookBySourceID(ctx, sourceID)
	switch {
	case noRows(err):
		return nil, nil
	case err != nil:
		return nil, failed("read webhook", err)
	}
	return toWebhook(row), nil
}

// Redirect writes the new address and gives it a clean start -- switched on,
// failure run cleared. The entity has already done the same to itself, and the
// statement repeats it rather than reading the entity's flags, so a redirect
// cannot carry over a state that belonged to the old address.
func (r *WebhookRepository) Redirect(ctx context.Context, w *webhook.Webhook) error {
	rows, err := r.q(ctx).RedirectWebhook(ctx, gen.RedirectWebhookParams{
		ID:          w.ID.String(),
		CallbackUrl: w.CallbackURL,
		UpdatedAt:   w.UpdatedAt,
	})
	return wrote(rows, err, "redirect webhook")
}

func (r *WebhookRepository) RecordSuccess(ctx context.Context, w *webhook.Webhook) error {
	rows, err := r.q(ctx).RecordWebhookSuccess(ctx, gen.RecordWebhookSuccessParams{
		ID:        w.ID.String(),
		UpdatedAt: w.UpdatedAt,
	})
	return wrote(rows, err, "record webhook success")
}

// RecordFailure counts in SQL and hands the new number back. The domain decides
// what it means; counting it here is what keeps it right when several callbacks
// for one source settle at the same moment.
func (r *WebhookRepository) RecordFailure(ctx context.Context, w *webhook.Webhook) (int, error) {
	count, err := r.q(ctx).RecordWebhookFailure(ctx, gen.RecordWebhookFailureParams{
		ID:        w.ID.String(),
		UpdatedAt: w.UpdatedAt,
	})
	switch {
	case noRows(err):
		return 0, errs.NotFoundErr("webhook not found").WithErr(webhook.ErrNotFound)
	case err != nil:
		return 0, failed("record webhook failure", err)
	}
	return int(count), nil
}

// Deactivate stops the callbacks without forgetting the address.
//
// Zero rows is success here, unlike everywhere else in this file. The statement
// only matches a row that is not already in the asked-for state, and two
// callbacks crossing the failure limit together both arrive here -- the second
// changed nothing and nothing is wrong. The cost is that a webhook deleted
// underneath us looks the same as one already switched off.
func (r *WebhookRepository) Deactivate(ctx context.Context, w *webhook.Webhook) error {
	return r.setActive(ctx, w, false)
}

// Activate is the way back, and the statement clears the failure run as it goes
// -- without that, a callback switched off for being dead would be switched off
// again by the first hiccup after it was fixed.
func (r *WebhookRepository) Activate(ctx context.Context, w *webhook.Webhook) error {
	return r.setActive(ctx, w, true)
}

func (r *WebhookRepository) setActive(
	ctx context.Context, w *webhook.Webhook, active bool,
) error {
	_, err := r.q(ctx).SetWebhookActive(ctx, gen.SetWebhookActiveParams{
		ID:        w.ID.String(),
		IsActive:  active,
		UpdatedAt: w.UpdatedAt,
	})
	if err != nil {
		return failed("set webhook active", err)
	}
	return nil
}

// wrote reports a statement that matched nothing as a missing webhook. These
// are all called with an entity that was just read, so no match means the row
// is gone rather than unchanged.
func wrote(rows int64, err error, op string) error {
	if err != nil {
		return failed(op, err)
	}
	if rows == 0 {
		return errs.NotFoundErr("webhook not found").WithErr(webhook.ErrNotFound).WithStr(op)
	}
	return nil
}

// --- mapping -----------------------------------------------------------------

// toWebhook needs no error path: the row carries no enum and no json, only text
// and numbers the domain accepts as they are.
func toWebhook(row gen.Webhook) *webhook.Webhook {
	return webhook.Restore(webhook.Snapshot{
		ID:                  shared.ID(row.ID),
		SourceID:            row.SourceID,
		CallbackURL:         row.CallbackUrl,
		IsActive:            row.IsActive,
		ConsecutiveFailures: int(row.ConsecutiveFailures),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	})
}

// WriteSecret replaces the sealed signing secret on one source's callback.
//
// It knows nothing about what is in that string: whether it is encrypted, with
// which key, and how a key change is survived all stop at the secret package.
func (r *WebhookRepository) WriteSecret(
	ctx context.Context, sourceID string, id shared.ID, secret string, now time.Time,
) error {
	rows, err := r.q(ctx).WriteWebhookSecret(ctx, gen.WriteWebhookSecretParams{
		ID:        id.String(),
		SourceID:  sourceID,
		Secret:    optional(secret),
		UpdatedAt: now,
	})
	return wrote(rows, err, "write webhook secret")
}

// ReadSecret hands back the sealed secret and the row it belongs to, and treats
// a source with no callback as one with no secret rather than as an error: the
// caller is asking whether it can sign, and "there is nothing here" is an
// answer to that.
func (r *WebhookRepository) ReadSecret(
	ctx context.Context, sourceID string,
) (shared.ID, string, error) {
	row, err := r.q(ctx).ReadWebhookSecret(ctx, sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", failed("read webhook secret", err)
	}
	if row.Secret == nil {
		return shared.ID(row.ID), "", nil
	}
	return shared.ID(row.ID), *row.Secret, nil
}
