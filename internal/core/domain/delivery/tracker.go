package delivery

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Tracker is the sending side: it finds deliveries that still have to go out,
// and writes down what happened to them.
//
// It holds a repository and a clock, and nothing else. No generator, because it
// never opens a delivery -- every one it touches was opened by the gateway. No
// publisher, because recovery sends rather than putting the event back on the
// broker, so nothing on this side ever publishes.
//
// That is the whole reason it is not Service: the dispatcher would otherwise be
// handed a broker connection it never uses, and the next person to read the
// wiring would reasonably assume it does.
type Tracker struct {
	repo Repository
	now  shared.NowFunc
}

func NewTracker(repo Repository, now shared.NowFunc) *Tracker {
	return &Tracker{repo: repo, now: now}
}

func (t *Tracker) Get(ctx context.Context, id shared.ID) (*Delivery, error) {
	return t.repo.ReadByID(ctx, id)
}

// ListAllForNotification returns every delivery of one message, unpaged. It is
// read when the last of them settles and the whole outcome goes out at once, so
// a page would be the wrong shape: the callback is about all of them or none.
func (t *Tracker) ListAllForNotification(
	ctx context.Context, notificationID shared.ID,
) ([]Delivery, error) {
	return t.repo.ListByNotificationID(ctx, notificationID)
}

// ClaimStale takes the deliveries nobody was told about: the rows whose publish
// never reached the broker.
//
// Taking rather than listing, because recovery sends directly and the broker's
// duplicate window never sees these. Whoever gets a row here owns it until the
// lease runs out or Release hands it back.
func (t *Tracker) ClaimStale(
	ctx context.Context, olderThan, lease time.Duration, limit int,
) ([]Delivery, error) {
	return t.repo.ClaimStale(ctx, olderThan, lease, limit)
}

// Release gives a claimed row back, so a failure that changed nothing does not
// hold it for the whole lease.
func (t *Tracker) Release(ctx context.Context, d *Delivery) error {
	return t.repo.Release(ctx, d)
}

// RecordSent stores the outcome. The sending already happened; this only writes
// down what it was.
func (t *Tracker) RecordSent(
	ctx context.Context, d *Delivery, providerMessageID string, attempts int,
) error {
	if err := d.MarkSent(providerMessageID, attempts, t.now()); err != nil {
		return err
	}
	return t.repo.Update(ctx, d)
}

// RecordFailure stores a final failure. A transient one is not recorded at all:
// the delivery stays pending and the broker retries it.
func (t *Tracker) RecordFailure(
	ctx context.Context, d *Delivery, reason FailureReason, detail string, attempts int,
) error {
	if err := d.MarkFailed(reason, detail, attempts, t.now()); err != nil {
		return err
	}
	return t.repo.Update(ctx, d)
}
