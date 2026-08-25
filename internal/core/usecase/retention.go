package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/pkg/errs"
)

// Retention drops what nobody is going to ask about again.
//
// srosha is not an archive. A message and its deliveries answer "what happened
// to this", and past some age nobody asks -- so keeping them is a table that
// only grows, and slower queries for everyone who does ask.
type Retention struct {
	notifs *notification.Service
	log    *slog.Logger

	// age is how long a message is kept. Deliveries are not mentioned because
	// they are not separate: the foreign key takes them with it.
	age   time.Duration
	batch int
}

func NewRetention(
	notifs *notification.Service, log *slog.Logger, age time.Duration, batch int,
) *Retention {
	return &Retention{notifs: notifs, log: log, age: age, batch: batch}
}

// Purge deletes in batches until nothing older is left.
//
// Batches, because an unbounded DELETE over a table that has been collecting for
// a year is one transaction holding locks on all of it. A run that carried on
// where the last one stopped would never catch up on a table that fell behind
// once, so this keeps going until a batch comes back short.
//
// The context is the scheduler's, so a shutdown stops it between batches rather
// than in the middle of one.
func (r *Retention) Purge(ctx context.Context) error {
	var total int

	for range maxRetentionBatches {
		// A shutdown between batches is not a failure: whatever went is gone,
		// and the next run carries on from there.
		select {
		case <-ctx.Done():
			r.log.InfoContext(ctx, "retention stopped early", "deleted", total)
			return nil
		default:
		}

		n, err := r.notifs.DeleteOlderThan(ctx, r.age, r.batch)
		if err != nil {
			// Whatever was already deleted stays deleted, which is fine: this
			// is not one operation but many, and the next run carries on.
			return errs.InternalErr("retention could not finish").
				WithErr(err).
				WithStr(fmt.Sprintf("deleted %d before failing", total))
		}

		total += n
		if n < r.batch {
			break // nothing older is left
		}
	}

	if total > 0 {
		r.log.InfoContext(ctx, "old messages deleted", "count", total, "older_than", r.age)
	}
	return nil
}
