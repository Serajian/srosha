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
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	maxAttempts        = 5
	maxWebhookFailures = 3
	reconcileAfter     = 5 * time.Minute
	reconcileGiveUp    = 30 * time.Minute
	reconcileLease     = 10 * time.Minute
)

type dispatchRig struct {
	dispatcher *usecase.Dispatcher
	deliveries *fakeDeliveries
	sender     *fakeSender
	deliveryID shared.ID
	base       *rig
	notifs     *fakeNotifications
	notifier   *fakeNotifier
	webhooks   *fakeWebhooks
}

// dispatchOpts is everything a dispatch test may want to change: the message
// that was submitted, what the sender does, what the registry answers, and what
// time it is when the dispatcher runs.
type dispatchOpts struct {
	cmd      usecase.SubmitCommand
	sender   *fakeSender
	registry fakeRegistry
	at       time.Time

	// callback registers a webhook for acme before the run; empty means none.
	callback  string
	notifyErr error
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
	r.deliveries.staleAt = o.at // one notion of "now" for the whole rig

	d := &dispatchRig{
		deliveries: r.deliveries,
		sender:     o.sender,
		deliveryID: ds[0].ID,
		base:       r,
		notifs:     r.notifs,
	}
	d.notifier = &fakeNotifier{err: o.notifyErr}
	d.webhooks = newFakeWebhooks()
	webhookSvc := webhook.NewService(d.webhooks, seqIDs(), clock, webhook.Strict)

	if o.callback != "" {
		if _, err := webhookSvc.Register(context.Background(), "acme",
			webhook.Registration{CallbackURL: o.callback}); err != nil {
			t.Fatalf("could not register the callback: %v", err)
		}
	}

	d.dispatcher = usecase.NewDispatcher(
		notification.NewService(r.notifs, seqIDs(), clock),
		delivery.NewTracker(r.deliveries, clock),
		webhookSvc,
		o.registry,
		d.notifier,
		seqIDs(),
		clock,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		maxAttempts, maxWebhookFailures,
		reconcileAfter, reconcileGiveUp, reconcileLease, 100,
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

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
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

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
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

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 2); err != nil {
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

	err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1)
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

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, maxAttempts); err != nil {
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

			if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
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

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err == nil {
		t.Fatal("Handle() returned nil, want the event asked for again")
	}

	if got := d.reload(t); got.Status() != delivery.StatusPending {
		t.Errorf("Status() = %v, want it left pending", got.Status())
	}
}

// An event naming a delivery that is not there cannot be fixed by trying again.
func TestHandleAcknowledgesAnUnknownDelivery(t *testing.T) {
	d := newDispatchRig(t, nil)
	unknown := shared.ID("01J8XQ2M4E7N9V3B5C6D7F8ZZZ")

	if err := d.dispatcher.Handle(context.Background(), unknown, 1); err != nil {
		t.Errorf("Handle() error = %v, want it acknowledged", err)
	}
	if d.sender.count() != 0 {
		t.Error("something was sent for a delivery that does not exist")
	}
}

// Recover finds a delivery the broker was never told about and sends it.
func TestRecoverSendsAStrandedDelivery(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.at = now.Add(10 * time.Minute) // older than reconcileAfter
	})

	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	if d.sender.count() == 0 {
		t.Fatal("nothing was sent")
	}
	if got := d.reload(t); got.Status() != delivery.StatusSent {
		t.Errorf("Status() = %v, want SENT", got.Status())
	}
}

// A row that has not been waiting long enough is left to the broker.
func TestRecoverLeavesFreshDeliveriesAlone(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.at = now.Add(time.Minute) // younger than reconcileAfter
	})

	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	if d.sender.count() != 0 {
		t.Error("a delivery still in the broker's hands was sent by recovery")
	}
}

// Age is the retry counter. Below the give-up threshold a transient failure
// writes nothing, so the row comes back older on the next run.
func TestRecoverLeavesAYoungFailureAlone(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.sender.err = &shared.SendError{Permanent: false, Detail: "connection reset"}
		o.at = now.Add(10 * time.Minute)
	})

	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v, want one row's failure not to stop it", err)
	}

	got := d.reload(t)
	if got.Status() != delivery.StatusPending {
		t.Errorf("Status() = %v, want it left pending", got.Status())
	}
	if got.Attempts() != 0 {
		t.Errorf("Attempts() = %d, want nothing written", got.Attempts())
	}
}

// Past the give-up threshold the same failure is final, so the source gets an
// answer instead of the row looping for ever.
func TestRecoverGivesUpOnAnOldFailure(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.sender.err = &shared.SendError{Permanent: false, Detail: "connection reset"}
		o.at = now.Add(reconcileGiveUp + time.Minute)
	})

	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	got := d.reload(t)
	if got.Status() != delivery.StatusFailed {
		t.Errorf("Status() = %v, want FAILED", got.Status())
	}
	if got.FailureReason() != delivery.FailureMaxAttempts {
		t.Errorf("FailureReason() = %v, want MAX_ATTEMPTS", got.FailureReason())
	}
}

// Already settled rows are not the recovery's business.
func TestRecoverIgnoresSettledDeliveries(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.cmd.Routes = []source.Route{{Channel: shared.ChannelEmail}} // just the one
		o.at = now.Add(time.Hour)
	})

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	sentOnce := d.sender.count()

	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	if d.sender.count() != sentOnce {
		t.Error("recovery sent a delivery that was already settled")
	}
}

// sendAnother submits a fresh single-recipient message and dispatches it, so a
// test can drive several complete messages through the same rig.
func (d *dispatchRig) sendAnother(t *testing.T) {
	t.Helper()

	c := cmd()
	c.Routes = []source.Route{{Channel: shared.ChannelEmail}}

	res, err := d.base.submitter.Submit(context.Background(), c)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	ds := d.base.deliveries.all(res.ID)
	if err := d.dispatcher.Handle(context.Background(), ds[0].ID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
}

// An event for a row that is gone must not come back. Returning the error would
// ask the broker for it again, once per backoff step, for something no attempt
// can find.
func TestAnEventForADeliveryThatIsGoneIsDone(t *testing.T) {
	d := newDispatchRig(t, nil)

	err := d.dispatcher.Handle(context.Background(), shared.ID("01J8XQ2M4E7N9V3B5C6D7F8999"), 1)
	if err != nil {
		t.Errorf("Handle() error = %v, want the broker to be told it is done", err)
	}
	if d.sender.count() != 0 {
		t.Error("something was sent for a delivery that does not exist")
	}
}

// A delivery whose message was deleted gets an outcome written down. Without
// that the row stays pending for ever: every attempt reads the same absence and
// records nothing against it.
func TestADeliveryWithoutAMessageIsFailedRatherThanRetried(t *testing.T) {
	d := newDispatchRig(t, nil)
	d.notifs.forget(d.reload(t).NotificationID)

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v, want the outcome written rather than retried", err)
	}

	got := d.reload(t)
	if got.Status() != delivery.StatusFailed {
		t.Errorf("status = %q, want FAILED", got.Status())
	}
	if got.FailureReason() != delivery.FailurePermanent {
		t.Errorf("reason = %q, want PERMANENT", got.FailureReason())
	}
}

// A claimed row must not come back on the next sweep. Recovery sends directly,
// so two sweeps holding the same delivery is somebody getting the message twice.
func TestASweptRowIsNotSweptAgain(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.at = now.Add(reconcileAfter + time.Minute)
		o.sender = &fakeSender{channel: shared.ChannelEmail, err: errors.New("provider is down")}
		o.registry = fakeRegistry{sender: o.sender}
	})

	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	first := d.sender.count()
	if first == 0 {
		t.Fatal("the first sweep sent nothing")
	}

	// The claim was released by the transient failure, so the row is available
	// again -- but only because it was released, not because claims are free.
	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if d.sender.count() != first*2 {
		t.Errorf("second sweep sent %d in total, want %d", d.sender.count(), first*2)
	}
}

// The other half: a row still held is skipped. Without the release above this
// is what every transient failure would look like until the lease ran out.
func TestARowStillHeldIsSkipped(t *testing.T) {
	at := now.Add(reconcileAfter + time.Minute)
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.at = at

		// One route, so the whole message is the row being held.
		one := cmd()
		one.Routes = one.Routes[:1]
		o.cmd = one
	})

	d.deliveries.hold(d.deliveryID, at)

	if err := d.dispatcher.Recover(context.Background()); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if d.sender.count() != 0 {
		t.Errorf("sent %d for a delivery somebody else was holding", d.sender.count())
	}
}
