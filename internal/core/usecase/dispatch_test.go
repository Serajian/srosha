package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

const maxAttempts = 5

type dispatchRig struct {
	dispatcher *usecase.Dispatcher
	deliveries *fakeDeliveries
	sender     *fakeSender
	event      shared.DispatchEvent
	deliveryID shared.ID
}

// dispatchOpts is everything a dispatch test may want to change: the message
// that was submitted, what the sender does, what the registry answers, and what
// time it is when the dispatcher runs.
type dispatchOpts struct {
	cmd      usecase.SubmitCommand
	sender   *fakeSender
	registry fakeRegistry
	at       time.Time
}

// newDispatchRig submits a real message first, so the dispatcher works on rows
// the rest of the core actually produced.
func newDispatchRig(t *testing.T, tweak func(*dispatchOpts)) *dispatchRig {
	t.Helper()

	r := newRig(t, nil)

	sender := &fakeSender{channel: shared.ChannelEmail, providerID: "tg-8821"}
	o := &dispatchOpts{
		cmd:      cmd(),
		sender:   sender,
		registry: fakeRegistry{sender: sender},
		at:       now,
	}
	if tweak != nil {
		tweak(o)
	}

	res, err := r.submitter.Submit(context.Background(), o.cmd)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	ds := r.deliveries.all(res.ID)
	clock := fixedNow(o.at)

	d := &dispatchRig{
		deliveries: r.deliveries,
		sender:     o.sender,
		deliveryID: ds[0].ID,
		event: shared.DispatchEvent{
			DeliveryID: ds[0].ID,
			SourceID:   "acme",
			Channel:    ds[0].Recipient.Channel,
			Priority:   shared.PriorityNormal,
		},
	}
	d.dispatcher = usecase.NewDispatcher(
		notification.NewService(r.notifs, seqIDs(), clock),
		delivery.NewService(r.deliveries, r.publisher, seqIDs(), clock),
		o.registry,
		clock,
		maxAttempts,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return d
}

func (d *dispatchRig) reload(t *testing.T) *delivery.Delivery {
	t.Helper()
	got, err := d.deliveries.ReadByID(context.Background(), d.deliveryID)
	if err != nil || got == nil {
		t.Fatalf("could not reload the delivery: %v", err)
	}
	return got
}

func TestHandleSendsAndRecordsTheOutcome(t *testing.T) {
	d := newDispatchRig(t, nil)

	if err := d.dispatcher.Handle(context.Background(), d.event, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if d.sender.count() != 1 {
		t.Fatalf("sent %d messages, want 1", d.sender.count())
	}
	got := d.reload(t)
	if got.Status() != delivery.StatusSent {
		t.Errorf("Status() = %v, want SENT", got.Status())
	}
	if got.ProviderMessageID() != "tg-8821" {
		t.Errorf("ProviderMessageID() = %q, want tg-8821", got.ProviderMessageID())
	}
	if got.Attempts() != 1 {
		t.Errorf("Attempts() = %d, want the broker's count", got.Attempts())
	}
}

// The message carries the recipient and the text, and nothing else.
func TestHandleSendsTheRightMessage(t *testing.T) {
	d := newDispatchRig(t, nil)

	if err := d.dispatcher.Handle(context.Background(), d.event, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	m := d.sender.sent[0]
	if m.Recipient.Address != "ops@acme.com" {
		t.Errorf("address = %q, want the resolved one", m.Recipient.Address)
	}
	if m.Body != "your order shipped" {
		t.Errorf("body = %q, want the message body", m.Body)
	}
}

// The duplicate guard: the broker is at-least-once, so a settled delivery must
// be acknowledged, not sent again.
func TestHandleIgnoresASettledDelivery(t *testing.T) {
	d := newDispatchRig(t, nil)

	if err := d.dispatcher.Handle(context.Background(), d.event, 1); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if err := d.dispatcher.Handle(context.Background(), d.event, 2); err != nil {
		t.Fatalf("redelivery returned an error: %v", err)
	}

	if d.sender.count() != 1 {
		t.Errorf("sent %d messages, want the second attempt refused", d.sender.count())
	}
}

// A transient failure writes nothing and asks for the event again. The delivery
// stays pending, which is also what the sweep looks for.
func TestHandleRetriesATransientFailure(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.sender.err = &shared.SendError{Permanent: false, Detail: "connection reset"}
	})

	err := d.dispatcher.Handle(context.Background(), d.event, 1)
	if err == nil {
		t.Fatal("Handle() returned nil, want the event asked for again")
	}

	got := d.reload(t)
	if got.Status() != delivery.StatusPending {
		t.Errorf("Status() = %v, want it left pending", got.Status())
	}
	if got.Attempts() != 0 {
		t.Errorf("Attempts() = %d, want nothing written", got.Attempts())
	}
}

// The last attempt is recorded rather than dropped, so the source can be told
// what happened instead of the event vanishing into a dead-letter queue.
func TestHandleRecordsTheLastAttempt(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.sender.err = &shared.SendError{Permanent: false, Detail: "connection reset"}
	})

	if err := d.dispatcher.Handle(context.Background(), d.event, maxAttempts); err != nil {
		t.Fatalf("Handle() error = %v, want the event acknowledged", err)
	}

	got := d.reload(t)
	if got.Status() != delivery.StatusFailed {
		t.Errorf("Status() = %v, want FAILED", got.Status())
	}
	if got.FailureReason() != delivery.FailureMaxAttempts {
		t.Errorf("FailureReason() = %v, want MAX_ATTEMPTS", got.FailureReason())
	}
}

func TestHandleRecordsFailures(t *testing.T) {
	tests := []struct {
		name   string
		tweak  func(*dispatchOpts)
		reason delivery.FailureReason
	}{
		{
			name: "the provider says it will never work",
			tweak: func(o *dispatchOpts) {
				o.sender.err = &shared.SendError{Permanent: true, Detail: "chat not found"}
			},
			reason: delivery.FailurePermanent,
		},
		{
			name: "no sending identity is set up",
			tweak: func(o *dispatchOpts) {
				o.registry.err = errs.InvalidInputErr("no sender configured for this channel")
			},
			reason: delivery.FailureNoSender,
		},
		{
			name: "the message expired before its turn came",
			tweak: func(o *dispatchOpts) {
				expires := now.Add(time.Hour)
				o.cmd.ExpireAt = &expires
				o.at = expires.Add(time.Minute)
			},
			reason: delivery.FailureExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDispatchRig(t, tt.tweak)

			if err := d.dispatcher.Handle(context.Background(), d.event, 1); err != nil {
				t.Fatalf("Handle() error = %v, want the event acknowledged", err)
			}

			got := d.reload(t)
			if got.Status() != delivery.StatusFailed {
				t.Errorf("Status() = %v, want FAILED", got.Status())
			}
			if got.FailureReason() != tt.reason {
				t.Errorf("FailureReason() = %v, want %v", got.FailureReason(), tt.reason)
			}
		})
	}
}

// A registry that fails for its own reasons -- the database behind it, say --
// is not a configuration answer and must be tried again.
func TestHandleRetriesWhenTheRegistryItselfFails(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.registry.err = errors.New("credential store unreachable")
	})

	if err := d.dispatcher.Handle(context.Background(), d.event, 1); err == nil {
		t.Fatal("Handle() returned nil, want the event asked for again")
	}

	if got := d.reload(t); got.Status() != delivery.StatusPending {
		t.Errorf("Status() = %v, want it left pending", got.Status())
	}
}

// An event naming a delivery that is not there cannot be fixed by trying again.
func TestHandleAcknowledgesAnUnknownDelivery(t *testing.T) {
	d := newDispatchRig(t, nil)
	e := d.event
	e.DeliveryID = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8ZZZ")

	if err := d.dispatcher.Handle(context.Background(), e, 1); err != nil {
		t.Errorf("Handle() error = %v, want it acknowledged", err)
	}
	if d.sender.count() != 0 {
		t.Error("something was sent for a delivery that does not exist")
	}
}
