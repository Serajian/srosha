package delivery

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Repository interface {
	CreateByList(ctx context.Context, ds []Delivery) error
	ReadByID(ctx context.Context, id shared.ID) (*Delivery, error)

	PageByNotificationID(
		ctx context.Context, notificationID shared.ID, c shared.Cursor,
	) (shared.Pagination[Delivery], error)

	// ListStale finds deliveries still pending past a cutoff: the ones whose
	// publish never reached the broker. It returns the rows themselves, because
	// the caller decides what to do from how long each has been waiting.
	ListStale(ctx context.Context, olderThan time.Duration, limit int) ([]Delivery, error)

	// Update writes one delivery. There is deliberately no whole-set save:
	// several workers settle deliveries of the same message at the same time,
	// and a set-wide write would clobber each other's results.
	Update(ctx context.Context, d *Delivery) error
}

// Publisher announces that a delivery is ready to be sent.
type Publisher interface {
	Publish(ctx context.Context, e shared.DispatchEvent) error
}

type Sender interface {
	Channel() shared.Channel

	// Send returns the provider's own id for the message. We do not track
	// delivery ourselves, so that id is the handle a source needs to do it.
	Send(ctx context.Context, m shared.Message) (providerMessageID string, err error)
}

// SenderRegistry hands back a sender already configured with the right identity.
// The core asks for one by name and never sees a token: which credential, how it
// is decrypted, and what client is built are all the adapter's business.
type SenderRegistry interface {
	For(ctx context.Context, sourceID string, c shared.Channel, name string) (Sender, error)
}
