package usecase_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// In-memory stand-ins for every port. If one of these is awkward to write, the
// port it implements is the thing to fix.

// seqIDs hands out a fixed sequence, so a test can name the ids it expects.
func seqIDs() shared.IDFunc {
	var (
		mu sync.Mutex
		n  int
	)
	return func() shared.ID {
		mu.Lock()
		defer mu.Unlock()
		n++
		return shared.ID(fmt.Sprintf("01J8XQ2M4E7N9V3B5C6D7F8%03d", n))
	}
}

func fixedNow(t time.Time) shared.NowFunc { return func() time.Time { return t } }

type fakeLimiter struct{ allow bool }

func (l fakeLimiter) Allow(context.Context, string) (bool, error) { return l.allow, nil }

type fakeSources struct{ byID map[string]*source.Source }

func (r fakeSources) ReadByID(_ context.Context, id string) (*source.Source, error) {
	s, ok := r.byID[id]
	if !ok {
		return nil, errs.NotFoundErr("source not found")
	}
	return s, nil
}

type fakeCredentials struct {
	byChannel map[shared.Channel][]credential.Credential
}

func (r fakeCredentials) ListBySourceAndChannel(
	_ context.Context, _ string, c shared.Channel,
) ([]credential.Credential, error) {
	return r.byChannel[c], nil
}

type fakeNotifications struct {
	mu   sync.Mutex
	byID map[shared.ID]*notification.Notification
}

func newFakeNotifications() *fakeNotifications {
	return &fakeNotifications{byID: map[shared.ID]*notification.Notification{}}
}

func (r *fakeNotifications) Create(_ context.Context, n *notification.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[n.ID] = n
	return nil
}

func (r *fakeNotifications) ReadByID(
	_ context.Context, id shared.ID,
) (*notification.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id], nil
}

func (r *fakeNotifications) ReadByIdempotencyKey(
	_ context.Context, sourceID, key string,
) (*notification.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.byID {
		if n.SourceID == sourceID && n.IdempotencyKey == key {
			return n, nil
		}
	}
	return nil, nil
}

type fakeDeliveries struct {
	mu        sync.Mutex
	byNotif   map[shared.ID][]delivery.Delivery
	createErr error

	// staleAt is "now" as far as ListStale is concerned.
	staleAt time.Time
}

func newFakeDeliveries() *fakeDeliveries {
	return &fakeDeliveries{byNotif: map[shared.ID][]delivery.Delivery{}}
}

func (r *fakeDeliveries) CreateByList(_ context.Context, ds []delivery.Delivery) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range ds {
		r.byNotif[d.NotificationID] = append(r.byNotif[d.NotificationID], d)
	}
	return nil
}

// ReadByID hands back a COPY, like a real repository would. Returning a pointer
// into the stored slice would make every change persist by accident, and a
// broken Update would look like it worked.
func (r *fakeDeliveries) ReadByID(_ context.Context, id shared.ID) (*delivery.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ds := range r.byNotif {
		for i := range ds {
			if ds[i].ID == id {
				got := ds[i]
				return &got, nil
			}
		}
	}
	return nil, nil
}

func (r *fakeDeliveries) Update(_ context.Context, d *delivery.Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ds := r.byNotif[d.NotificationID]
	for i := range ds {
		if ds[i].ID == d.ID {
			ds[i] = *d
			return nil
		}
	}
	return errs.NotFoundErr("delivery not found")
}

func (r *fakeDeliveries) PageByNotificationID(
	_ context.Context, id shared.ID, c shared.Cursor,
) (shared.Pagination[delivery.Delivery], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	all := r.byNotif[id]
	start := 0
	if c.After != nil {
		for i, d := range all {
			if d.ID == *c.After {
				start = i + 1
				break
			}
		}
	}

	end := min(start+c.Limit, len(all))
	page := shared.Pagination[delivery.Delivery]{}
	for i := start; i < end; i++ {
		page.Items = append(page.Items, &all[i])
	}
	if end < len(all) && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1].ID
		page.NextCursor = &last
	}
	return page, nil
}

func (r *fakeDeliveries) ListByNotificationID(
	_ context.Context, id shared.ID,
) ([]delivery.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]delivery.Delivery, len(r.byNotif[id]))
	copy(out, r.byNotif[id])
	return out, nil
}

func (r *fakeDeliveries) ListStale(
	_ context.Context, olderThan time.Duration, limit int,
) ([]delivery.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []delivery.Delivery
	for _, ds := range r.byNotif {
		for _, d := range ds {
			if d.IsSettled() || r.staleAt.Sub(d.UpdatedAt()) < olderThan {
				continue
			}
			out = append(out, d)
			if len(out) == limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (r *fakeDeliveries) all(id shared.ID) []delivery.Delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byNotif[id]
}

// fakeUOW runs the callback directly. Whether the writes were really atomic is
// the adapter's problem; what the use case must get right is putting them
// inside one call, which this still proves.
type fakeUOW struct{ err error }

func (u fakeUOW) Atomically(ctx context.Context, fn func(context.Context) error) error {
	if u.err != nil {
		return u.err
	}
	return fn(ctx)
}

type fakePublisher struct {
	mu        sync.Mutex
	published []shared.DispatchEvent
	err       error
}

func (p *fakePublisher) Publish(_ context.Context, e shared.DispatchEvent) error {
	if p.err != nil {
		return p.err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published = append(p.published, e)
	return nil
}

func (p *fakePublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.published)
}

type fakeSender struct {
	channel    shared.Channel
	providerID string
	err        error

	mu   sync.Mutex
	sent []shared.Message
}

func (s *fakeSender) Channel() shared.Channel { return s.channel }

func (s *fakeSender) Send(_ context.Context, m shared.Message) (string, error) {
	s.mu.Lock()
	s.sent = append(s.sent, m)
	s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	return s.providerID, nil
}

func (s *fakeSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

// fakeRegistry hands back one sender for every channel, or refuses.
type fakeRegistry struct {
	sender *fakeSender
	err    error
}

func (r fakeRegistry) For(
	context.Context, string, shared.Channel, string,
) (delivery.Sender, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.sender, nil
}

type fakeWebhooks struct {
	mu       sync.Mutex
	bySource map[string]*webhook.Webhook
}

func newFakeWebhooks() *fakeWebhooks {
	return &fakeWebhooks{bySource: map[string]*webhook.Webhook{}}
}

func (r *fakeWebhooks) Create(_ context.Context, w *webhook.Webhook) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bySource[w.SourceID] = w
	return nil
}

// ReadBySourceID hands back a copy, so only Update makes a change stick.
func (r *fakeWebhooks) ReadBySourceID(_ context.Context, id string) (*webhook.Webhook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.bySource[id]
	if !ok {
		return nil, nil
	}
	got := *w
	return &got, nil
}

func (r *fakeWebhooks) Update(_ context.Context, w *webhook.Webhook) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bySource[w.SourceID]; !ok {
		return errs.NotFoundErr("webhook not found")
	}
	got := *w
	r.bySource[w.SourceID] = &got
	return nil
}

type fakeNotifier struct {
	mu      sync.Mutex
	batches []webhook.Batch
	err     error
}

func (n *fakeNotifier) Notify(_ context.Context, _ *webhook.Webhook, b webhook.Batch) error {
	if n.err != nil {
		return n.err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.batches = append(n.batches, b)
	return nil
}

func (n *fakeNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.batches)
}

func (n *fakeNotifier) last() webhook.Batch {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.batches[len(n.batches)-1]
}
