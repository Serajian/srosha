package delivery

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

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

func (s *Service) Get(ctx context.Context, id shared.ID) (*Delivery, error) {
	return s.repo.ReadByID(ctx, id)
}

func (s *Service) ListForNotification(
	ctx context.Context, notificationID shared.ID, c shared.Cursor,
) (shared.Pagination[Delivery], error) {
	return s.repo.PageByNotificationID(ctx, notificationID, c.Normalize())
}

func (s *Service) ListStale(
	ctx context.Context, olderThan time.Duration, limit int,
) ([]shared.ID, error) {
	return s.repo.ListStale(ctx, olderThan, limit)
}

// RecordSent stores the outcome. The sending already happened; this only writes
// down what it was.
func (s *Service) RecordSent(
	ctx context.Context, d *Delivery, providerMessageID string, attempts int,
) error {
	if err := d.MarkSent(providerMessageID, attempts, s.now()); err != nil {
		return err
	}
	return s.repo.Update(ctx, d)
}

// RecordFailure stores a final failure. A transient one is not recorded at all:
// the delivery stays pending and the broker retries it.
func (s *Service) RecordFailure(
	ctx context.Context, d *Delivery, reason FailureReason, detail string, attempts int,
) error {
	if err := d.MarkFailed(reason, detail, attempts, s.now()); err != nil {
		return err
	}
	return s.repo.Update(ctx, d)
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
