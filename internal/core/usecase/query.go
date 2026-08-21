package usecase

import (
	"context"
	"fmt"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

type QueryResult struct {
	Notification *notification.Notification
	Deliveries   shared.Pagination[delivery.Delivery]
}

type Querier struct {
	notifs     *notification.Service
	deliveries *delivery.Service
}

func NewQuerier(notifs *notification.Service, deliveries *delivery.Service) *Querier {
	return &Querier{notifs: notifs, deliveries: deliveries}
}

// Get answers "what happened to my message".
//
// A message belonging to another source is reported as not found, not as
// forbidden: telling a caller that an id exists but is not theirs lets them
// discover which ids exist.
func (q *Querier) Get(
	ctx context.Context, sourceID string, id shared.ID, c shared.Cursor,
) (QueryResult, error) {
	n, err := q.notifs.Get(ctx, id)
	if err != nil {
		return QueryResult{}, err
	}
	if n == nil || n.SourceID != sourceID {
		return QueryResult{}, errs.NotFoundErr("notification not found").
			WithErr(notification.ErrNotFound).
			WithStr(fmt.Sprintf("id %q, source %q", id, sourceID))
	}

	page, err := q.deliveries.ListForNotification(ctx, id, c)
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{Notification: n, Deliveries: page}, nil
}
