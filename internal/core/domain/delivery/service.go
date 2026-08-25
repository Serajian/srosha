package delivery

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Service is the accepting side: it opens deliveries and announces them.
//
// It is separate from Tracker because the two run in different binaries and
// need different things. The gateway creates and publishes, so it needs a
// generator and a broker; the dispatcher only reads and records, and giving it
// a publisher it never calls would be handing it a dependency it does not have.
//
// The split is clean: no method here is one Tracker has.
type Service struct {
	repo  Repository
	pub   Publisher
	newID shared.IDFunc
	now   shared.NowFunc
}

func NewService(repo Repository, pub Publisher, newID shared.IDFunc, now shared.NowFunc) *Service {
	return &Service{repo: repo, pub: pub, newID: newID, now: now}
}

// Create validates the recipient set and stores one delivery per entry.
func (s *Service) Create(
	ctx context.Context,
	notificationID shared.ID,
	recipients []shared.Recipient,
	senders map[shared.Channel]string,
) ([]Delivery, error) {
	ds, err := NewSet(notificationID, recipients, senders, s.newID, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateByList(ctx, ds); err != nil {
		return nil, err
	}
	return ds, nil
}

// Publish announces one delivery. Its failure is not fatal: the pending row is
// the record that this must be sent, and the sweep picks it up again.
func (s *Service) Publish(
	ctx context.Context, d Delivery, sourceID string, p shared.Priority,
) error {
	return s.pub.Publish(ctx, shared.DispatchEvent{
		DeliveryID: d.ID,
		SourceID:   sourceID,
		Channel:    d.Recipient.Channel,
		Priority:   p,
	})
}

// ListForNotification is one page of a message's deliveries, for the source
// asking what happened to it.
func (s *Service) ListForNotification(
	ctx context.Context, notificationID shared.ID, c shared.Cursor,
) (shared.Pagination[Delivery], error) {
	return s.repo.PageByNotificationID(ctx, notificationID, c.Normalize())
}
