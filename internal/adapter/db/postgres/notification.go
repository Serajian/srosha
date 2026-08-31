package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NotificationRepository implements notification.Repository.
type NotificationRepository struct{ base }

func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{base{pool: pool}}
}

// Create stores the message, and reports the one case the caller has to handle
// differently: somebody stored the same idempotency key first.
//
// The use case checks for the key before building anything, but a client that
// timed out and retried is exactly what puts two requests between that check and
// this write. The statement takes the conflict rather than raising it, so zero
// rows means "theirs won" -- an outcome, not a fault.
func (r *NotificationRepository) Create(ctx context.Context, n *notification.Notification) error {
	metadata, err := fromMetadata(n.Metadata())
	if err != nil {
		return badRow("notification", n.ID.String(), "metadata", err)
	}

	rows, err := r.q(ctx).CreateNotification(ctx, gen.CreateNotificationParams{
		ID:                n.ID.String(),
		SourceID:          n.SourceID,
		IdempotencyKey:    optional(n.IdempotencyKey),
		SourceName:        n.SourceName,
		Title:             n.Title,
		Body:              n.Body,
		RequestedPriority: n.RequestedPriority.String(),
		EffectivePriority: n.EffectivePriority.String(),
		ExpireAt:          n.ExpireAt,
		Metadata:          metadata,
		CreatedAt:         n.CreatedAt,
	})
	if err != nil {
		return failed("create notification", err)
	}
	if rows == 0 {
		return errs.DuplicateErr("this request has already been accepted").
			WithErr(notification.ErrDuplicateKey).
			WithStr(n.IdempotencyKey)
	}
	return nil
}

func (r *NotificationRepository) ReadByID(
	ctx context.Context, id shared.ID,
) (*notification.Notification, error) {
	row, err := r.q(ctx).ReadNotification(ctx, id.String())
	switch {
	case noRows(err):
		return nil, errs.NotFoundErr("notification not found").WithErr(notification.ErrNotFound)
	case err != nil:
		return nil, failed("read notification", err)
	}
	return toNotification(row)
}

// ReadByIdempotencyKey answers with a nil entity when the key is unused. That is
// the port's contract and it matters: "never seen" and "could not look" have to
// stay distinguishable, and an error for the first would make the caller treat a
// perfectly ordinary first request as a failure.
// DeleteOlderThan drops one batch of messages written before a moment, and says
// how many went.
//
// Their deliveries go with them: the foreign key is ON DELETE CASCADE, so there
// is no second statement to keep in step with this one.
//
// One batch, not all of them. An unbounded DELETE over a table that has been
// collecting for a year is a single transaction holding locks on the whole of
// it; the caller decides how many batches a run is worth.
func (r *NotificationRepository) DeleteOlderThan(
	ctx context.Context, before time.Time, limit int,
) (int, error) {
	rows, err := r.q(ctx).DeleteNotificationsBefore(ctx, gen.DeleteNotificationsBeforeParams{
		Before:   before,
		RowLimit: int32(limit), //nolint:gosec // a batch size, bounded by config
	})
	if err != nil {
		return 0, failed("delete old notifications", err)
	}
	return int(rows), nil
}

// PageBySource answers "what did I send", newest first.
//
// Newest first, unlike every other listing here: a source asking this wants what
// it just sent. The cursor therefore walks backwards through the ids, which is
// backwards through time -- a ULID orders by both.
//
// A moment rather than a window: what "last week" means is a rule, and a rule
// belongs above the statement that reads rows. This one only fetches.
//
// There is no upper bound. Every window reaches back from now, so there is
// nothing to bound the other end with.
func (r *NotificationRepository) PageBySource(
	ctx context.Context, sourceID string, since time.Time, c shared.Cursor,
) (shared.Pagination[notification.Notification], error) {
	c = c.Normalize()

	var after *string
	if c.After != nil {
		after = optional(c.After.String())
	}

	rows, err := r.q(ctx).PageNotificationsBySource(ctx, gen.PageNotificationsBySourceParams{
		SourceID: sourceID,
		After:    after,
		From:     &since,
		Until:    nil,
		RowLimit: int32(c.Limit) + 1, //nolint:gosec // clamped by Normalize
	})
	if err != nil {
		return shared.Pagination[notification.Notification]{}, failed("page notifications", err)
	}

	var next *shared.ID
	if len(rows) > c.Limit {
		rows = rows[:c.Limit]
		last := shared.ID(rows[len(rows)-1].ID)
		next = &last
	}

	items := make([]*notification.Notification, 0, len(rows))
	for _, row := range rows {
		n, err := toNotification(row)
		if err != nil {
			return shared.Pagination[notification.Notification]{}, err
		}
		items = append(items, n)
	}
	return shared.Pagination[notification.Notification]{Items: items, NextCursor: next}, nil
}

func (r *NotificationRepository) ReadByIdempotencyKey(
	ctx context.Context, sourceID, key string,
) (*notification.Notification, error) {
	row, err := r.q(ctx).ReadNotificationByIdempotencyKey(
		ctx, gen.ReadNotificationByIdempotencyKeyParams{
			SourceID:       sourceID,
			IdempotencyKey: optional(key),
		})
	switch {
	case noRows(err):
		return nil, nil
	case err != nil:
		return nil, failed("read notification by idempotency key", err)
	}
	return toNotification(row)
}

// ListForOperator answers what an operator may see of a source's messages:
// when, on what, how it went -- never what it said. The statement selects no
// title and no body, so there is nothing here to carry either by mistake.
func (r *NotificationRepository) ListForOperator(
	ctx context.Context, sourceID string, limit int,
) ([]notification.OperatorRow, error) {
	rows, err := r.q(ctx).ListMessagesForOperator(ctx, gen.ListMessagesForOperatorParams{
		SourceID: sourceID,
		RowLimit: int32(limit), //nolint:gosec // a page size, bounded by the caller
	})
	if err != nil {
		return nil, failed("list messages for operator", err)
	}

	out := make([]notification.OperatorRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, notification.OperatorRow{
			ID:        shared.ID(row.ID),
			Channels:  row.Channels,
			Failed:    int(row.Failed),
			Total:     int(row.Total),
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// --- mapping -----------------------------------------------------------------

func toNotification(row gen.Notification) (*notification.Notification, error) {
	requested, err := shared.ParsePriority(row.RequestedPriority)
	if err != nil {
		return nil, badRow("notification", row.ID, "requested_priority", err)
	}

	effective, err := shared.ParsePriority(row.EffectivePriority)
	if err != nil {
		return nil, badRow("notification", row.ID, "effective_priority", err)
	}

	metadata, err := toMetadata(row.Metadata)
	if err != nil {
		return nil, badRow("notification", row.ID, "metadata", err)
	}

	// Restore takes the entity plus the map it keeps private, so the mapper
	// hands over a value rather than reaching into unexported state.
	return notification.Restore(notification.Notification{
		ID:                shared.ID(row.ID),
		SourceID:          row.SourceID,
		IdempotencyKey:    deref(row.IdempotencyKey),
		SourceName:        row.SourceName,
		Title:             row.Title,
		Body:              row.Body,
		RequestedPriority: requested,
		EffectivePriority: effective,
		ExpireAt:          row.ExpireAt,
		CreatedAt:         row.CreatedAt,
	}, metadata), nil
}

func toMetadata(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func fromMetadata(metadata map[string]string) ([]byte, error) {
	if metadata == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(metadata)
}
