package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Dispatcher sends one delivery and writes down what happened.
//
// It has two ways in and one way through. The broker brings an event; the
// scheduler finds a row nobody was told about. Recovery is not a different
// operation, only the same one found a different way, which is why both end up
// in deliver.
type Dispatcher struct {
	notifs     *notification.Service
	deliveries *delivery.Service
	senders    delivery.SenderRegistry
	now        shared.NowFunc
	log        *slog.Logger

	// maxAttempts must match the broker's own delivery limit. Set it higher and
	// the broker gives up first, leaving the row pending with no outcome.
	maxAttempts int

	// after is how long a delivery may sit pending before Recover picks it up;
	// giveUp is the age past which its next attempt is its last.
	after  time.Duration
	giveUp time.Duration
	batch  int
}

func NewDispatcher(
	notifs *notification.Service,
	deliveries *delivery.Service,
	senders delivery.SenderRegistry,
	now shared.NowFunc,
	log *slog.Logger,
	maxAttempts int,
	after, giveUp time.Duration,
	batch int,
) *Dispatcher {
	return &Dispatcher{
		notifs: notifs, deliveries: deliveries, senders: senders,
		now: now, log: log,
		maxAttempts: maxAttempts, after: after, giveUp: giveUp, batch: batch,
	}
}

// Handle sends one delivery on the broker's behalf.
//
// Returning nil means the broker is done with this event, whether the message
// went out or failed for good. Returning an error asks for it again.
//
// attempt is the broker's own delivery count, not ours. Counting here would
// miss the attempt whose worker died mid-send, and that message would retry for
// ever.
func (d *Dispatcher) Handle(ctx context.Context, id shared.ID, attempt int) error {
	del, err := d.deliveries.Get(ctx, id)
	if err != nil {
		return err
	}
	if del == nil {
		// Nothing to send and nothing to fix by trying again.
		d.log.ErrorContext(ctx, "event for a delivery that does not exist", "delivery_id", id)
		return nil
	}

	// The duplicate guard. The broker is at-least-once, so the same event can
	// arrive after the work is done; this is the expected case, not a fault.
	if del.IsSettled() {
		return nil
	}

	return d.deliver(ctx, del, attempt >= d.maxAttempts)
}

// Recover deals with deliveries nobody was told about: the rows written when
// the publish never reached the broker.
//
// One row's failure must not stop the rest, so errors are logged and the loop
// carries on. The next run picks up whatever is still waiting.
func (d *Dispatcher) Recover(ctx context.Context) error {
	stale, err := d.deliveries.ListStale(ctx, d.after, d.batch)
	if err != nil {
		return err
	}

	now := d.now()
	for i := range stale {
		del := &stale[i]

		// Nothing is written when a send fails transiently, so the row only
		// gets older. Its age is how long this has been stuck, which is why no
		// attempt counter is needed here.
		lastChance := now.Sub(del.UpdatedAt()) >= d.giveUp

		if err := d.deliver(ctx, del, lastChance); err != nil {
			d.log.WarnContext(ctx, "recovery attempt failed",
				"delivery_id", del.ID, "last_chance", lastChance, "err", err)
		}
	}
	return nil
}

// deliver is the one path out. Both ways in end here, so SENT and FAILED mean
// the same thing however the delivery was found.
//
// lastChance says what a transient failure costs: nothing when there is time
// left, a recorded FAILED when there is not.
func (d *Dispatcher) deliver(
	ctx context.Context, del *delivery.Delivery, lastChance bool,
) error {
	n, err := d.notifs.Get(ctx, del.NotificationID)
	if err != nil {
		return err
	}
	if n == nil {
		d.log.ErrorContext(ctx, "delivery without a message",
			"delivery_id", del.ID, "notification_id", del.NotificationID)
		return d.fail(ctx, del, delivery.FailurePermanent, "message is gone")
	}

	if n.IsExpired(d.now()) {
		return d.fail(ctx, del, delivery.FailureExpired, "")
	}

	sender, err := d.senders.For(ctx, n.SourceID, del.Recipient.Channel, del.SenderName)
	if err != nil {
		// A configuration answer -- no such identity, none set up -- will read
		// the same on every retry. Anything else is the lookup itself failing.
		if errs.IsType(err, errs.ErrInvalidInput) {
			return d.fail(ctx, del, delivery.FailureNoSender, err.Error())
		}
		return err
	}

	providerID, err := sender.Send(ctx, shared.Message{
		Recipient: del.Recipient,
		Title:     n.Title,
		Body:      n.Body,
	})
	if err != nil {
		return d.sendFailed(ctx, del, err, lastChance)
	}

	return settled(d.deliveries.RecordSent(ctx, del, providerID, del.Attempts()+1))
}

// sendFailed decides whether the send is worth another go.
func (d *Dispatcher) sendFailed(
	ctx context.Context, del *delivery.Delivery, err error, lastChance bool,
) error {
	if shared.IsPermanentSend(err) {
		return d.fail(ctx, del, delivery.FailurePermanent, err.Error())
	}
	if lastChance {
		// Recording it rather than letting the attempt vanish means the outcome
		// is stored and the source can be told.
		return d.fail(ctx, del, delivery.FailureMaxAttempts, err.Error())
	}

	// Transient, with time left: write nothing at all and let it come back. The
	// delivery stays pending, which is also what Recover looks for, and its age
	// keeps growing towards the last chance.
	d.log.WarnContext(ctx, "send failed, will try again", "delivery_id", del.ID, "err", err)
	return err
}

func (d *Dispatcher) fail(
	ctx context.Context, del *delivery.Delivery, reason delivery.FailureReason, detail string,
) error {
	return settled(d.deliveries.RecordFailure(ctx, del, reason, detail, del.Attempts()+1))
}

// settled turns "this delivery already moved" into success. Another worker got
// there first, which is the expected end of a redelivered event, not a failure.
func settled(err error) error {
	if errors.Is(err, delivery.ErrInvalidTransition) {
		return nil
	}
	return err
}
