package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

var now = time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)

// testRetention is deliberately shorter than a month, so a test asking for
// WindowLastMonth is asking for more than this deployment keeps.
const testRetention = 7 * 24 * time.Hour

func acmeSource() *source.Source {
	return &source.Source{
		ID:                 "acme",
		Name:               "Acme Payments",
		MaxPriority:        shared.PriorityHigh,
		IsActive:           true,
		AllowCustomAddress: true,
		DefaultAddresses: map[shared.Channel]string{
			shared.ChannelEmail: "ops@acme.com",
		},
	}
}

type options struct {
	allow      bool
	creds      map[shared.Channel][]credential.Credential
	uowErr     error
	publishErr error
	createErr  error
}

type rig struct {
	submitter  *usecase.Submitter
	querier    *usecase.Querier
	notifs     *fakeNotifications
	deliveries *fakeDeliveries
	publisher  *fakePublisher
	src        *source.Source
}

func newRig(t *testing.T, tweak func(*rig, *options)) *rig {
	t.Helper()

	o := &options{allow: true, creds: map[shared.Channel][]credential.Credential{}}
	r := &rig{
		notifs:     newFakeNotifications(),
		deliveries: newFakeDeliveries(),
		publisher:  &fakePublisher{},
		src:        acmeSource(),
	}
	if tweak != nil {
		tweak(r, o)
	}
	r.deliveries.createErr = o.createErr
	r.publisher.err = o.publishErr

	ids, clock := seqIDs(), fixedNow(now)

	srcSvc := source.NewService(
		fakeSources{byID: map[string]*source.Source{"acme": r.src}},
		fakeLimiter{allow: o.allow},
	)
	credSvc := credential.NewService(newFakeCredentials(o.creds), fixedNow(now))
	notifSvc := notification.NewService(r.notifs, ids, clock, testRetention)
	delSvc := delivery.NewService(r.deliveries, r.publisher, ids, clock)

	r.submitter = usecase.NewSubmitter(
		srcSvc, credSvc, notifSvc, delSvc,
		fakeUOW{err: o.uowErr},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	r.querier = usecase.NewQuerier(notifSvc, delSvc)
	return r
}

func cmd() usecase.SubmitCommand {
	return usecase.SubmitCommand{
		SourceID: "acme",
		Body:     "your order shipped",
		Priority: shared.PriorityNormal,
		Routes: []source.Route{
			{Channel: shared.ChannelEmail},
			{Channel: shared.ChannelTelegram, Address: "123456789"},
		},
	}
}

func TestSubmitWritesTheMessageAndOneDeliveryPerRoute(t *testing.T) {
	r := newRig(t, nil)

	got, err := r.submitter.Submit(context.Background(), cmd())
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	n, _ := r.notifs.ReadByID(context.Background(), got.ID)
	if n == nil {
		t.Fatal("the message was not written")
	}
	if n.SourceID != "acme" || n.SourceName != "Acme Payments" {
		t.Errorf("origin not taken from the source: %q / %q", n.SourceID, n.SourceName)
	}

	ds := r.deliveries.all(got.ID)
	if len(ds) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(ds))
	}
	if ds[0].Recipient.Address != "ops@acme.com" {
		t.Errorf("email address = %q, want the source's default", ds[0].Recipient.Address)
	}
	if ds[1].Recipient.Address != "123456789" {
		t.Errorf("telegram address = %q, want the explicit one", ds[1].Recipient.Address)
	}
	if r.publisher.count() != 2 {
		t.Errorf("published %d events, want one per delivery", r.publisher.count())
	}
}

func TestSubmitReportsTheDowngrade(t *testing.T) {
	r := newRig(t, nil)
	c := cmd()
	c.Priority = shared.PriorityCritical

	got, err := r.submitter.Submit(context.Background(), c)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if got.EffectivePriority != shared.PriorityHigh || !got.Downgraded {
		t.Errorf("priority = %v, downgraded = %v; want HIGH and true",
			got.EffectivePriority, got.Downgraded)
	}
}

// A client that retries a timed-out request must not get a second message.
func TestSubmitIsIdempotent(t *testing.T) {
	r := newRig(t, nil)
	c := cmd()
	c.IdempotencyKey = "order-4471"

	first, err := r.submitter.Submit(context.Background(), c)
	if err != nil {
		t.Fatalf("first Submit() error = %v", err)
	}
	before := r.publisher.count()

	second, err := r.submitter.Submit(context.Background(), c)
	if err != nil {
		t.Fatalf("second Submit() error = %v", err)
	}

	if second.ID != first.ID || !second.Duplicate {
		t.Errorf("id = %q duplicate = %v; want the original and true", second.ID, second.Duplicate)
	}
	if len(r.deliveries.all(first.ID)) != 2 {
		t.Error("the second call created deliveries again")
	}
	if r.publisher.count() != before {
		t.Error("the second call published again")
	}
}

// This is the whole point of committing before publishing. The rows are
// written; a broker that is down is the sweep's problem, not the client's.
func TestSubmitSucceedsEvenWhenPublishFails(t *testing.T) {
	r := newRig(t, func(_ *rig, o *options) { o.publishErr = errors.New("nats is down") })

	got, err := r.submitter.Submit(context.Background(), cmd())
	if err != nil {
		t.Fatalf("Submit() failed because publishing did: %v", err)
	}
	if len(r.deliveries.all(got.ID)) != 2 {
		t.Error("the deliveries were not written")
	}
}

// And the reverse: if the write fails, nothing may be announced.
func TestSubmitPublishesNothingWhenTheWriteFails(t *testing.T) {
	for _, tt := range []struct {
		name  string
		tweak func(*rig, *options)
	}{
		{"transaction fails", func(_ *rig, o *options) { o.uowErr = errors.New("db is down") }},
		{"deliveries fail", func(_ *rig, o *options) { o.createErr = errors.New("constraint") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.tweak)

			if _, err := r.submitter.Submit(context.Background(), cmd()); err == nil {
				t.Fatal("Submit() succeeded with a failed write")
			}
			if r.publisher.count() != 0 {
				t.Error("an event was published for work that never committed")
			}
		})
	}
}

func TestSubmitRefuses(t *testing.T) {
	tests := []struct {
		name     string
		tweak    func(*rig, *options)
		mutate   func(*usecase.SubmitCommand)
		sentinel error
		typ      errs.Type
	}{
		{
			name:     "over the rate limit",
			tweak:    func(_ *rig, o *options) { o.allow = false },
			sentinel: source.ErrRateLimited, typ: errs.ErrTooMany,
		},
		{
			name:     "inactive source",
			tweak:    func(r *rig, _ *options) { r.src.IsActive = false },
			sentinel: source.ErrSourceInactive, typ: errs.ErrForbidden,
		},
		{
			name:     "no routes",
			mutate:   func(c *usecase.SubmitCommand) { c.Routes = nil },
			sentinel: source.ErrNoRoutes, typ: errs.ErrInvalidInput,
		},
		{
			name:  "custom address from a source that may not name one",
			tweak: func(r *rig, _ *options) { r.src.AllowCustomAddress = false },
			mutate: func(c *usecase.SubmitCommand) {
				c.Routes = []source.Route{
					{Channel: shared.ChannelEmail, Address: "someone@else.com"},
				}
			},
			sentinel: source.ErrCustomAddressNotAllowed, typ: errs.ErrForbidden,
		},
		{
			name: "channel the source has no address for",
			mutate: func(c *usecase.SubmitCommand) {
				c.Routes = []source.Route{{Channel: shared.ChannelWhatsApp}}
			},
			sentinel: source.ErrNoAddressForChannel, typ: errs.ErrInvalidInput,
		},
		{
			name:     "empty body",
			mutate:   func(c *usecase.SubmitCommand) { c.Body = "" },
			sentinel: notification.ErrEmptyBody, typ: errs.ErrInvalidInput,
		},
		{
			// A typo in the name lands here too, which is the point: it is
			// caught now rather than hours later as a failed delivery.
			name: "a sending identity that does not exist",
			mutate: func(c *usecase.SubmitCommand) {
				c.Senders = map[shared.Channel]string{shared.ChannelEmail: "newsletter"}
			},
			sentinel: credential.ErrNotFound, typ: errs.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.tweak)
			c := cmd()
			if tt.mutate != nil {
				tt.mutate(&c)
			}

			_, err := r.submitter.Submit(context.Background(), c)
			if err == nil {
				t.Fatal("Submit() succeeded, want an error")
			}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("errors.Is(%v) = false, got %v", tt.sentinel, err)
			}
			if !errs.IsType(err, tt.typ) {
				t.Errorf("type = %v, want %v", errs.TypeOf(err), tt.typ)
			}
			if r.publisher.count() != 0 {
				t.Error("a refused request still published")
			}
		})
	}
}

func TestSubmitStampsAKnownSenderName(t *testing.T) {
	r := newRig(t, func(_ *rig, o *options) {
		o.creds = map[shared.Channel][]credential.Credential{
			shared.ChannelEmail: {*credential.Restore(credential.Snapshot{
				ID: "01J8XQ2M4E7N9V3B5C6D7F8C01", SourceID: "acme",
				Channel: shared.ChannelEmail, Name: "marketing", IsActive: true,
			})},
		}
	})
	c := cmd()
	c.Senders = map[shared.Channel]string{shared.ChannelEmail: "marketing"}

	got, err := r.submitter.Submit(context.Background(), c)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	ds := r.deliveries.all(got.ID)
	if ds[0].SenderName != "marketing" || ds[1].SenderName != "" {
		t.Errorf("sender names = %q / %q; want marketing and the default",
			ds[0].SenderName, ds[1].SenderName)
	}
}

// A client that timed out and retried puts two requests either side of the
// idempotency check, so the second one only learns the key is taken when it
// writes. That must come back as the original message, not as an error.
func TestSubmitLosingTheIdempotencyRace(t *testing.T) {
	winner, err := notification.New(
		shared.ID("01J8XKQ2R7M3NB4PZC5VD6WHW1"),
		notification.Origin{
			ID:          "acme",
			Name:        "Acme Payments",
			MaxPriority: shared.PriorityCritical,
		},
		notification.Request{
			IdempotencyKey: "order-42",
			Title:          "t",
			Body:           "b",
			Priority:       shared.PriorityHigh,
		},
		now,
	)
	if err != nil {
		t.Fatalf("building the winner: %v", err)
	}

	r := newRig(t, func(r *rig, _ *options) { r.notifs.loseRaceTo = winner })

	c := cmd()
	c.IdempotencyKey = "order-42"

	got, err := r.submitter.Submit(context.Background(), c)
	if err != nil {
		t.Fatalf("Submit() = %v, want the original message", err)
	}
	if !got.Duplicate {
		t.Error("Duplicate = false, want true")
	}
	if got.ID != winner.ID {
		t.Errorf("ID = %q, want the winner's %q", got.ID, winner.ID)
	}
}
