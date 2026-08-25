package delivery

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Repository interface {
	CreateByList(ctx context.Context, ds []Delivery) error
	ReadByID(ctx context.Context, id shared.ID) (*Delivery, error)

	// ListByNotificationID returns every delivery of one message. Used when the
	// last of them settles and the whole outcome goes out at once.
	ListByNotificationID(ctx context.Context, notificationID shared.ID) ([]Delivery, error)

	// ClaimStale takes the deliveries still pending past a cutoff: the ones whose
	// publish never reached the broker. It returns the rows themselves, because
	// the caller decides what to do from how long each has been waiting.
	//
	// It CLAIMS them, and that is the whole point. Recovery sends directly
	// rather than republishing, so the broker's duplicate window never sees
	// these -- two sweeps running at once would both send. The claim is what
	// makes a second dispatcher possible.
	//
	// lease is how long a claim is good for. A dispatcher that dies mid-send
	// would otherwise strand the row for ever, so the claim expires; a send that
	// merely failed calls Release, so the lease means one thing only.
	ClaimStale(ctx context.Context, olderThan, lease time.Duration, limit int) ([]Delivery, error)

	// ClaimAnnouncement decides who tells the source that a message is finished.
	//
	// The callback goes out when the LAST delivery settles, and two workers
	// settling the last two at the same moment both see a finished message. The
	// contract is that a callback is sent once and never retried, so exactly one
	// of them may send it -- and this is what says which.
	//
	// True means the announcement is this caller's. False means somebody already
	// has it, which is an answer and not a failure.
	ClaimAnnouncement(ctx context.Context, notificationID shared.ID, now time.Time) (bool, error)

	// Release hands a claimed row back before its lease is up. A transient
	// failure writes nothing, so without this the row would sit unclaimable
	// until the lease expired -- and the lease would quietly become the retry
	// interval, giving a row fewer attempts than the configuration says.
	Release(ctx context.Context, d *Delivery) error

	PageByNotificationID(
		ctx context.Context, notificationID shared.ID, c shared.Cursor,
	) (shared.Pagination[Delivery], error)

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
