package usecase

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

type Dispatcher struct {
	notifs      *notification.Service
	deliveries  *delivery.Service
	senders     delivery.SenderRegistry
	now         shared.NowFunc
	maxAttempts int
	log         *slog.Logger
}

func NewDispatcher(
	notifs *notification.Service,
	deliveries *delivery.Service,
	senders delivery.SenderRegistry,
	now shared.NowFunc,
	maxAttempts int,
	log *slog.Logger,
) *Dispatcher {
	return &Dispatcher{
		notifs: notifs, deliveries: deliveries, senders: senders,
		now: now, maxAttempts: maxAttempts, log: log,
	}
}

// Handle sends one delivery.
//
// Returning nil means the broker is done with this event, whether the message
// went out or failed for good. Returning an error asks for it again.
//
// attempt is the broker's own delivery count, not ours. Counting here would
// miss the attempt whose worker died mid-send, and that message would retry for
// ever.
func (d *Dispatcher) Handle(ctx context.Context, e shared.DispatchEvent, attempt int) error {
	del, err := d.deliveries.Get(ctx, e.DeliveryID)
	if err != nil {
		return err
	}
	if del == nil {
		// Nothing to send and nothing to fix by trying again.
		d.log.ErrorContext(ctx, "event for a delivery that does not exist",
			"delivery_id", e.DeliveryID)
		return nil
	}

	// The duplicate guard. The broker is at-least-once, so the same event can
	// arrive after the work is done; this is the expected case, not a fault.
	if del.IsSettled() {
		return nil
	}

	n, err := d.notifs.Get(ctx, del.NotificationID)
	if err != nil {
		return err
	}
	if n == nil {
		d.log.ErrorContext(ctx, "delivery without a message",
			"delivery_id", del.ID, "notification_id", del.NotificationID)
		return d.fail(ctx, del, delivery.FailurePermanent, "message is gone", attempt)
	}

	if n.IsExpired(d.now()) {
		return d.fail(ctx, del, delivery.FailureExpired, "", attempt)
	}

	sender, err := d.senders.For(ctx, n.SourceID, del.Recipient.Channel, del.SenderName)
	if err != nil {
		// A configuration answer -- no such identity, none set up -- will read
		// the same on every retry. Anything else is the lookup itself failing.
		if errs.IsType(err, errs.ErrInvalidInput) {
			return d.fail(ctx, del, delivery.FailureNoSender, err.Error(), attempt)
		}
		return err
	}

	providerID, err := sender.Send(ctx, shared.Message{
		Recipient: del.Recipient,
		Title:     n.Title,
		Body:      n.Body,
	})
	if err != nil {
		return d.sendFailed(ctx, del, err, attempt)
	}

	return d.settle(d.deliveries.RecordSent(ctx, del, providerID, attempt))
}

// sendFailed decides whether the send is worth another go.
func (d *Dispatcher) sendFailed(
	ctx context.Context, del *delivery.Delivery, err error, attempt int,
) error {
	if shared.IsPermanentSend(err) {
		return d.fail(ctx, del, delivery.FailurePermanent, err.Error(), attempt)
	}

	// The last attempt. Recording it here rather than letting the broker drop
	// the event means the outcome is stored and the source can be told.
	if d.maxAttempts > 0 && attempt >= d.maxAttempts {
		return d.fail(ctx, del, delivery.FailureMaxAttempts, err.Error(), attempt)
	}

	// Transient, with attempts left: write nothing at all and ask for it again.
	// The delivery stays pending, which is also what the sweep looks for.
	d.log.WarnContext(ctx, "send failed, will retry",
		"delivery_id", del.ID, "attempt", attempt, "err", err)
	return err
}

func (d *Dispatcher) fail(
	ctx context.Context, del *delivery.Delivery,
	reason delivery.FailureReason, detail string, attempt int,
) error {
	return d.settle(d.deliveries.RecordFailure(ctx, del, reason, detail, attempt))
}

// settle turns "this delivery already moved" into success. Another worker got
// there first, which is the expected end of a redelivered event, not a failure.
func (d *Dispatcher) settle(err error) error {
	if errors.Is(err, delivery.ErrInvalidTransition) {
		return nil
	}
	return err
}
