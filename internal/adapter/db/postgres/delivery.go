package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DeliveryRepository implements delivery.Repository.
//
// It takes a clock because ListStale is asked for an age rather than a moment,
// and turning one into the other needs to know now. Injected rather than read
// from the wall so a test can put a delivery an hour in the past without waiting.
type DeliveryRepository struct {
	base
	now shared.NowFunc
}

func NewDeliveryRepository(pool *pgxpool.Pool, now shared.NowFunc) *DeliveryRepository {
	return &DeliveryRepository{base: base{pool: pool}, now: now}
}

// CreateByList writes every recipient of one message in a single copy, so a
// message to four channels costs one round trip rather than four.
func (r *DeliveryRepository) CreateByList(ctx context.Context, ds []delivery.Delivery) error {
	if len(ds) == 0 {
		return nil
	}

	rows := make([]gen.CreateDeliveriesParams, 0, len(ds))
	for i := range ds {
		d := &ds[i]
		rows = append(rows, gen.CreateDeliveriesParams{
			ID:             d.ID.String(),
			NotificationID: d.NotificationID.String(),
			Channel:        string(d.Recipient.Channel),
			Address:        d.Recipient.Address,
			SenderName:     d.SenderName,
			CreatedAt:      d.UpdatedAt(),
			UpdatedAt:      d.UpdatedAt(),
		})
	}

	written, err := r.q(ctx).CreateDeliveries(ctx, rows)
	if err != nil {
		return failed("create deliveries", err)
	}
	// Copy reports what it wrote, and a short count means rows were dropped
	// without an error -- a message that would then be missing a recipient
	// nobody ever hears about.
	if int(written) != len(rows) {
		return errs.InternalErr("not every delivery was written").
			WithStr(fmt.Sprintf("wrote %d of %d", written, len(rows)))
	}
	return nil
}

func (r *DeliveryRepository) ReadByID(
	ctx context.Context, id shared.ID,
) (*delivery.Delivery, error) {
	row, err := r.q(ctx).ReadDelivery(ctx, id.String())
	switch {
	case noRows(err):
		return nil, errs.NotFoundErr("delivery not found").WithErr(delivery.ErrNotFound)
	case err != nil:
		return nil, failed("read delivery", err)
	}
	return toDelivery(row)
}

func (r *DeliveryRepository) ListByNotificationID(
	ctx context.Context, notificationID shared.ID,
) ([]delivery.Delivery, error) {
	rows, err := r.q(ctx).ListDeliveriesByNotificationID(ctx, notificationID.String())
	if err != nil {
		return nil, failed("list deliveries", err)
	}
	return toDeliveries(rows)
}

// ClaimStale takes what recovery has to deal with, and takes it exclusively.
//
// The ages are turned into moments here: the port asks in terms of how long a
// row has been waiting and how long a claim is good for, because that is what
// the policy is written in.
//
// Whoever calls this owns the rows it returns until the lease runs out or they
// are released. Nothing else will pick them up in between.
func (r *DeliveryRepository) ClaimStale(
	ctx context.Context, olderThan, lease time.Duration, limit int,
) ([]delivery.Delivery, error) {
	now := r.now()

	rows, err := r.q(ctx).ClaimStaleDeliveries(ctx, gen.ClaimStaleDeliveriesParams{
		Now:                now,
		OlderThan:          now.Add(-olderThan),
		ClaimExpiredBefore: now.Add(-lease),
		RowLimit:           int32(limit), //nolint:gosec // a batch size, bounded by config
	})
	if err != nil {
		return nil, failed("claim stale deliveries", err)
	}
	return toDeliveries(rows)
}

// ClaimAnnouncement decides whether this caller is the one that tells the source.
//
// True means the announcement is theirs and nobody else will make it. False
// means somebody already has it -- not a failure, and the expected answer when
// two workers settle the last two deliveries of a message at the same moment.
func (r *DeliveryRepository) ClaimAnnouncement(
	ctx context.Context, notificationID shared.ID, now time.Time,
) (bool, error) {
	rows, err := r.q(ctx).ClaimNotificationAnnouncement(ctx, gen.ClaimNotificationAnnouncementParams{
		NotificationID: notificationID.String(),
		NotifiedAt:     now,
	})
	if err != nil {
		return false, failed("claim notification announcement", err)
	}
	return rows > 0, nil
}

// Release hands a row back before its lease is up, so a transient failure does
// not make the lease the retry interval.
//
// Finding no row is not a failure: the delivery settled while this was in
// flight, and a settled row is not one recovery looks at.
func (r *DeliveryRepository) Release(ctx context.Context, d *delivery.Delivery) error {
	if _, err := r.q(ctx).ReleaseDeliveryClaim(ctx, d.ID.String()); err != nil {
		return failed("release delivery claim", err)
	}
	return nil
}

// PageByNotificationID asks for one row more than the caller wanted. That extra
// row is the whole answer to "is there another page", and it costs nothing next
// to a second query that counts.
func (r *DeliveryRepository) PageByNotificationID(
	ctx context.Context, notificationID shared.ID, c shared.Cursor,
) (shared.Pagination[delivery.Delivery], error) {
	c = c.Normalize()

	var after *string
	if c.After != nil {
		after = optional(c.After.String())
	}

	rows, err := r.q(ctx).PageDeliveriesByNotificationID(
		ctx, gen.PageDeliveriesByNotificationIDParams{
			NotificationID: notificationID.String(),
			After:          after,
			RowLimit:       int32(c.Limit) + 1, //nolint:gosec // clamped by Normalize
		})
	if err != nil {
		return shared.Pagination[delivery.Delivery]{}, failed("page deliveries", err)
	}

	var next *shared.ID
	if len(rows) > c.Limit {
		rows = rows[:c.Limit]
		last := shared.ID(rows[len(rows)-1].ID)
		next = &last
	}

	items := make([]*delivery.Delivery, 0, len(rows))
	for _, row := range rows {
		d, err := toDelivery(row)
		if err != nil {
			return shared.Pagination[delivery.Delivery]{}, err
		}
		items = append(items, d)
	}
	return shared.Pagination[delivery.Delivery]{Items: items, NextCursor: next}, nil
}

// Update writes an outcome, and reports the one case the caller must not treat
// as a failure: the row had already been settled by somebody else.
//
// That is what at-least-once produces. Two workers each hold a pending copy,
// both send, and both write; the statement only matches a row that is still
// pending, so the second changes nothing and is told so.
func (r *DeliveryRepository) Update(ctx context.Context, d *delivery.Delivery) error {
	rows, err := r.q(ctx).UpdateDelivery(ctx, gen.UpdateDeliveryParams{
		ID:                d.ID.String(),
		Status:            string(d.Status()),
		Attempts:          int32(d.Attempts()), //nolint:gosec // bounded by the broker's MaxDeliver
		LastError:         optional(d.LastError()),
		FailureReason:     optional(string(d.FailureReason())),
		ProviderMessageID: optional(d.ProviderMessageID()),
		UpdatedAt:         d.UpdatedAt(),
	})
	if err != nil {
		return failed("update delivery", err)
	}
	if rows == 0 {
		return errs.DuplicateErr("this delivery has already been settled").
			WithErr(delivery.ErrAlreadySettled).
			WithStr(d.ID.String())
	}
	return nil
}

// --- mapping -----------------------------------------------------------------

func toDelivery(row gen.Delivery) (*delivery.Delivery, error) {
	// The CHECK constraints make these unreachable in practice. They are still
	// checked, because the alternative is a Status the domain has never heard of
	// traveling further into it as if it were fine.
	status := delivery.Status(row.Status)
	if !status.Valid() {
		return nil, badRow("delivery", row.ID, "status", delivery.ErrUnknownStatus)
	}

	reason := delivery.FailureReason(deref(row.FailureReason))
	if reason != "" && !reason.Valid() {
		return nil, badRow("delivery", row.ID, "failure_reason", delivery.ErrUnknownFailureReason)
	}

	return delivery.Restore(delivery.Snapshot{
		ID:             shared.ID(row.ID),
		NotificationID: shared.ID(row.NotificationID),
		Recipient: shared.Recipient{
			Channel: shared.Channel(row.Channel),
			Address: row.Address,
		},
		SenderName:        row.SenderName,
		Status:            status,
		Attempts:          int(row.Attempts),
		LastError:         deref(row.LastError),
		FailureReason:     reason,
		ProviderMessageID: deref(row.ProviderMessageID),
		NotifiedAt:        row.NotifiedAt,
		UpdatedAt:         row.UpdatedAt,
	}), nil
}

func toDeliveries(rows []gen.Delivery) ([]delivery.Delivery, error) {
	out := make([]delivery.Delivery, 0, len(rows))
	for _, row := range rows {
		d, err := toDelivery(row)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, nil
}
