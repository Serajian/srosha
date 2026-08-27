package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
)

const callbackURL = "https://acme.com/hooks"

// oneRoute keeps a message to a single recipient, so settling it settles the
// whole thing.
func oneRoute(o *dispatchOpts) {
	o.cmd.Routes = []source.Route{{Channel: shared.ChannelEmail}}
	o.callback = callbackURL
}

func TestAnnounceWhenTheWholeMessageIsSettled(t *testing.T) {
	d := newDispatchRig(t, oneRoute)

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if d.notifier.count() != 1 {
		t.Fatalf("sent %d callbacks, want 1", d.notifier.count())
	}
	b := d.notifier.last()
	if len(b.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(b.Results))
	}
	if b.Results[0].Status != "SENT" {
		t.Errorf("status = %q, want SENT", b.Results[0].Status)
	}
	if b.Results[0].DeliveryID != d.deliveryID {
		t.Error("the result does not name the delivery it is about")
	}
	if b.Results[0].ProviderMessageID != "tg-8821" {
		t.Errorf("provider id = %q, want it passed through", b.Results[0].ProviderMessageID)
	}
}

// Two recipients, one settled: nothing goes out until the second one finishes.
func TestAnnounceWaitsForEveryRecipient(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) { o.callback = callbackURL })

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if d.notifier.count() != 0 {
		t.Error("a callback went out with one recipient still pending")
	}
}

// The provider's own words never leave the process: the result carries a reason
// the client can act on and nothing else.
func TestAnnounceCarriesNoProviderText(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		oneRoute(o)
		o.sender.err = &shared.SendError{
			Kind:   shared.SendPermanent,
			Detail: "chat not found: bot@internal",
		}
	})

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	r := d.notifier.last().Results[0]
	if r.Status != "FAILED" {
		t.Errorf("status = %q, want FAILED", r.Status)
	}
	if r.Reason != string(delivery.FailurePermanent) {
		t.Errorf("reason = %q, want PERMANENT", r.Reason)
	}
}

// No callback registered is not a failure, and must not stop the send being
// recorded.
func TestAnnounceDoesNothingWithoutACallback(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		o.cmd.Routes = []source.Route{{Channel: shared.ChannelEmail}}
	})

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if d.notifier.count() != 0 {
		t.Error("something was sent without a registered callback")
	}
	if got := d.reload(t); got.Status() != delivery.StatusSent {
		t.Errorf("Status() = %v, want the send still recorded", got.Status())
	}
}

// The callback is best effort. Its failure must not undo the delivery or be
// reported to the broker.
func TestAnnounceFailureDoesNotAffectTheDelivery(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		oneRoute(o)
		o.notifyErr = errors.New("customer endpoint is down")
	})

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() returned an error because the callback failed: %v", err)
	}

	if got := d.reload(t); got.Status() != delivery.StatusSent {
		t.Errorf("Status() = %v, want SENT", got.Status())
	}
}

// A dead endpoint would otherwise be called once for every message from now on.
func TestAnnounceSwitchesOffADeadCallback(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		oneRoute(o)
		o.notifyErr = errors.New("customer endpoint is down")
	})

	// A settled delivery is never announced twice, so the run has to come from
	// several messages -- which is what it would be in real life.
	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	for range maxWebhookFailures - 1 {
		d.sendAnother(t)
	}

	w, err := d.webhooks.ReadBySourceID(context.Background(), "acme")
	if err != nil {
		t.Fatalf("could not read the webhook: %v", err)
	}
	if w.IsActive() {
		t.Errorf("still active after %d failures in a row", maxWebhookFailures)
	}
}

func TestAnnounceReportsWhenItWasSent(t *testing.T) {
	d := newDispatchRig(t, func(o *dispatchOpts) {
		oneRoute(o)
		o.at = now.Add(time.Hour)
	})

	if err := d.dispatcher.Handle(context.Background(), d.deliveryID, 1); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	b := d.notifier.last()
	if !b.SentAt.Equal(now.Add(time.Hour)) {
		t.Errorf("SentAt = %v, want the clock we were given", b.SentAt)
	}
	if b.ID.IsZero() {
		t.Error("the batch has no id to trace it by")
	}
}
