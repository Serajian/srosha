package port

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
)

// UnitOfWork runs everything inside fn in one transaction. The core decides
// what must be atomic; the adapter decides how, and passes the transaction
// down through ctx so the repositories pick it up.
//
// A message and its deliveries are written through this. Two transactions
// would be two failures, and the second one leaves a message nobody delivers.
type UnitOfWork interface {
	Atomically(ctx context.Context, fn func(ctx context.Context) error) error
}

type NotificationRepository interface {
	Create(ctx context.Context, n *notification.Notification) error
	FindByID(ctx context.Context, id shared.ID) (*notification.Notification, error)

	// FindByIdempotencyKey returns nil when the key has not been used, so the
	// caller can tell "seen before" from "failed to look".
	FindByIdempotencyKey(ctx context.Context, sourceID, key string) (*notification.Notification, error)
}

type DeliveryRepository interface {
	CreateSet(ctx context.Context, ds []delivery.Delivery) error

	// LoadForDispatch returns the delivery and the message it belongs to. One
	// call, because the dispatcher needs the state and the text together and a
	// second round trip buys nothing.
	LoadForDispatch(ctx context.Context, id shared.ID) (*delivery.Delivery, *notification.Notification, error)

	// Save writes one delivery's outcome. There is deliberately no whole-set
	// save: several workers settle deliveries of the same message at the same
	// time, and a set-wide write would clobber each other's results.
	Save(ctx context.Context, d *delivery.Delivery) error

	// ListForNotification answers "what happened to my message". Paged, because
	// one message may have a thousand recipients.
	ListForNotification(
		ctx context.Context, id shared.ID, c shared.Cursor,
	) (shared.Pagination[delivery.Delivery], error)

	// ListStale finds deliveries still pending past a cutoff: the ones whose
	// publish never reached the broker. Republishing is safe because the broker
	// deduplicates on the message id.
	ListStale(ctx context.Context, olderThan time.Duration, limit int) ([]shared.ID, error)
}

type SourceRepository interface {
	FindByID(ctx context.Context, id string) (*source.Source, error)
}

type CredentialRepository interface {
	// ListFor returns every identity this source has on a channel, for Pick to
	// choose from and for the gateway to check a name against at submit time.
	ListFor(ctx context.Context, sourceID string, c shared.Channel) ([]credential.Credential, error)
}

type WebhookRepository interface {
	FindBySource(ctx context.Context, sourceID string) (*webhook.Webhook, error)
	Save(ctx context.Context, w *webhook.Webhook) error
}
