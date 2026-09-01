package usecase_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
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

func (r fakeSources) Create(_ context.Context, s *source.Source) error {
	r.byID[s.ID] = s
	return nil
}

func (r fakeSources) ListByOwner(
	_ context.Context, ownerID shared.ID,
) ([]source.Source, error) {
	out := []source.Source{}
	for _, s := range r.byID {
		if s.OwnerUserID == ownerID {
			out = append(out, *s)
		}
	}
	return out, nil
}

// UpdateSettings writes back over the stored source, so a test can read what an
// edit actually left behind rather than what the call returned.
func (r fakeSources) UpdateSettings(_ context.Context, s *source.Source) error {
	if _, ok := r.byID[s.ID]; !ok {
		return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	r.byID[s.ID] = s
	return nil
}

// ReadByID hands back a copy, as postgres would: a row read out of storage,
// not a live handle into it. Returning the stored pointer directly would let
// a caller's mutation land in the map before any Update call, which would
// make every write guard below unobservable -- exactly the failure mode this
// fake exists to catch.
func (r fakeSources) ReadByID(_ context.Context, id string) (*source.Source, error) {
	s, ok := r.byID[id]
	if !ok {
		return nil, errs.NotFoundErr("source not found")
	}
	got := *s
	return &got, nil
}

// UpdateReview writes only the columns the real statement writes. The fake
// mirroring that is the point: a test that saved the whole struct would pass
// even if the use case had carried something else in, such as a rename riding
// along with an approval.
func (r fakeSources) UpdateReview(_ context.Context, s *source.Source) error {
	row, ok := r.byID[s.ID]
	if !ok {
		return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	row.IsActive = s.IsActive
	row.ApprovedAt = s.ApprovedAt
	row.ReviewedAt = s.ReviewedAt
	row.ReviewNote = s.ReviewNote
	row.UpdatedAt = s.UpdatedAt
	return nil
}

func (r fakeSources) ListForReview(_ context.Context, limit int32) ([]source.Source, error) {
	out := []source.Source{}
	for _, s := range r.byID {
		if !s.IsReviewed() {
			out = append(out, *s)
		}
	}
	return capRows(out, limit), nil
}

func (r fakeSources) ListAll(_ context.Context, limit int32) ([]source.Source, error) {
	out := []source.Source{}
	for _, s := range r.byID {
		out = append(out, *s)
	}
	return capRows(out, limit), nil
}

// capRows mirrors what every LIMIT clause in this package's real statements
// does: never more rows than asked for. A fake that ignored the limit would
// let a use case's own truncate logic go untested by every test that uses it.
func capRows[T any](rows []T, limit int32) []T {
	if int32(len(rows)) > limit {
		return rows[:limit]
	}
	return rows
}

// fakeCredentials keeps the identities by id, which is what the port asks for
// now that a source can list, disable and rotate its own.
type fakeCredentials struct {
	mu   sync.Mutex
	byID map[shared.ID]credential.Credential
}

func newFakeCredentials(byChannel map[shared.Channel][]credential.Credential) *fakeCredentials {
	r := &fakeCredentials{byID: map[shared.ID]credential.Credential{}}
	for _, list := range byChannel {
		for _, c := range list {
			r.byID[c.ID] = c
		}
	}
	return r
}

func (r *fakeCredentials) all(sourceID string) []credential.Credential {
	out := make([]credential.Credential, 0, len(r.byID))
	for _, c := range r.byID {
		if c.SourceID == sourceID {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b credential.Credential) int {
		if a.Channel != b.Channel {
			return strings.Compare(a.Channel.String(), b.Channel.String())
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func (r *fakeCredentials) ListBySourceAndChannel(
	_ context.Context, sourceID string, c shared.Channel,
) ([]credential.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []credential.Credential
	for _, got := range r.all(sourceID) {
		if got.Channel == c {
			out = append(out, got)
		}
	}
	return out, nil
}

func (r *fakeCredentials) ListBySourceID(
	_ context.Context, sourceID string,
) ([]credential.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.all(sourceID), nil
}

// ReadByID is scoped by source, as postgres is: a guessed id must find nothing
// rather than somebody else's identity.
func (r *fakeCredentials) ReadByID(
	_ context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.byID[id]
	if !ok || c.SourceID != sourceID {
		return nil, errs.NotFoundErr("no credential with that id").WithErr(credential.ErrNotFound)
	}
	return &c, nil
}

func (r *fakeCredentials) Deactivate(_ context.Context, c *credential.Credential) error {
	return r.save(c)
}

func (r *fakeCredentials) Activate(_ context.Context, c *credential.Credential) error {
	return r.save(c)
}

// SetDefault refuses an inactive identity, as the statement does: a default that
// cannot be used is the one state a channel must never be left in.
func (r *fakeCredentials) SetDefault(_ context.Context, c *credential.Credential) error {
	if !c.IsActive() {
		return errs.InvalidInputErr("an inactive credential cannot be the default").
			WithErr(credential.ErrDefaultUnusable)
	}
	return r.save(c)
}

func (r *fakeCredentials) ClearDefault(
	_ context.Context, sourceID string, ch shared.Channel, now time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, c := range r.byID {
		if c.SourceID == sourceID && c.Channel == ch && c.IsDefault() {
			cleared := *credential.Restore(snapshotOf(&c, false, c.IsActive(), now))
			r.byID[id] = cleared
		}
	}
	return nil
}

func (r *fakeCredentials) save(c *credential.Credential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[c.ID] = *c
	return nil
}

func snapshotOf(
	c *credential.Credential, isDefault, isActive bool, now time.Time,
) credential.Snapshot {
	return credential.Snapshot{
		ID: c.ID, SourceID: c.SourceID, Channel: c.Channel, Name: c.Name,
		IsDefault: isDefault, IsActive: isActive,
		CreatedAt: c.CreatedAt, UpdatedAt: now,
	}
}

type fakeNotifications struct {
	mu   sync.Mutex
	byID map[shared.ID]*notification.Notification

	// loseRaceTo stands in for another request that stored this key between our
	// check and our write. Create refuses once and leaves the message behind,
	// which is exactly what the database does.
	loseRaceTo *notification.Notification

	// deliveries is what ListForOperator joins against. This fake has no join
	// of its own -- it is handed the other store directly and reads it, the
	// same two stores the real statement joins, kept apart the way the ports
	// keep them apart. Nil is fine for every test that never calls Messages.
	deliveries *fakeDeliveries
}

func newFakeNotifications() *fakeNotifications {
	return &fakeNotifications{byID: map[shared.ID]*notification.Notification{}}
}

func (r *fakeNotifications) Create(_ context.Context, n *notification.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if winner := r.loseRaceTo; winner != nil {
		r.loseRaceTo = nil
		r.byID[winner.ID] = winner
		return notification.ErrDuplicateKey
	}

	r.byID[n.ID] = n
	return nil
}

func (r *fakeNotifications) ReadByID(
	_ context.Context, id shared.ID,
) (*notification.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, ok := r.byID[id]
	if !ok {
		return nil, errs.NotFoundErr("notification not found").WithErr(notification.ErrNotFound)
	}
	return n, nil
}

// PageBySource is newest first, as postgres is, and honors the lower bound: a
// fake that ignored either would let a listing that ordered or filtered wrongly
// pass.
func (r *fakeNotifications) PageBySource(
	_ context.Context, sourceID string, since time.Time, c shared.Cursor,
) (shared.Pagination[notification.Notification], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	c = c.Normalize()

	var all []*notification.Notification
	for _, n := range r.byID {
		switch {
		case n.SourceID != sourceID:
		case n.CreatedAt.Before(since):
		case c.After != nil && n.ID >= *c.After:
		default:
			all = append(all, n)
		}
	}
	slices.SortFunc(all, func(a, b *notification.Notification) int {
		return strings.Compare(string(b.ID), string(a.ID)) // newest first
	})

	var next *shared.ID
	if len(all) > c.Limit {
		all = all[:c.Limit]
		last := all[len(all)-1].ID
		next = &last
	}
	return shared.Pagination[notification.Notification]{Items: all, NextCursor: next}, nil
}

// put and count let a test write a message aged by hand and see what survived.
func (r *fakeNotifications) put(n *notification.Notification) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[n.ID] = n
}

func (r *fakeNotifications) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// DeleteOlderThan deletes one batch, oldest first, and reports how many went.
// Batching is the behavior the use case depends on -- it keeps going until a
// batch comes back short -- so a fake that ignored the limit would let a purge
// that never loops pass.
func (r *fakeNotifications) DeleteOlderThan(
	_ context.Context, before time.Time, limit int,
) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var old []shared.ID
	for id, n := range r.byID {
		if n.CreatedAt.Before(before) {
			old = append(old, id)
		}
	}
	slices.Sort(old)

	if len(old) > limit {
		old = old[:limit]
	}
	for _, id := range old {
		delete(r.byID, id)
	}
	return len(old), nil
}

// forget deletes a message while its deliveries are still around, which is what
// a retention job or a manual clean-up does.
func (r *fakeNotifications) forget(id shared.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
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

// ListForOperator mirrors the join it stands in for: every message of one
// source, newest first, each carrying the channel and failure counts read out
// of the deliveries store handed to it. Never Title, never Body -- there is
// nowhere on notification.OperatorRow to put either.
func (r *fakeNotifications) ListForOperator(
	_ context.Context, sourceID string, limit int,
) ([]notification.OperatorRow, error) {
	r.mu.Lock()
	var all []*notification.Notification
	for _, n := range r.byID {
		if n.SourceID == sourceID {
			all = append(all, n)
		}
	}
	r.mu.Unlock()

	slices.SortFunc(all, func(a, b *notification.Notification) int {
		return strings.Compare(string(b.ID), string(a.ID)) // newest first
	})
	if len(all) > limit {
		all = all[:limit]
	}

	out := make([]notification.OperatorRow, 0, len(all))
	for _, n := range all {
		var channels []string
		failed, total := 0, 0
		if r.deliveries != nil {
			for _, d := range r.deliveries.all(n.ID) {
				total++
				if d.Status() == delivery.StatusFailed {
					failed++
				}
				ch := string(d.Recipient.Channel)
				if !slices.Contains(channels, ch) {
					channels = append(channels, ch)
				}
			}
		}
		out = append(out, notification.OperatorRow{
			ID: n.ID, Channels: channels, Failed: failed, Total: total, CreatedAt: n.CreatedAt,
		})
	}
	return out, nil
}

type fakeDeliveries struct {
	mu        sync.Mutex
	byNotif   map[shared.ID][]delivery.Delivery
	createErr error

	// staleAt is "now" as far as ClaimStale is concerned.
	staleAt time.Time

	// claimed is who holds what. Kept because the use case now depends on a
	// claimed row not coming back, and a fake that handed the same row out twice
	// would let a broken Recover pass.
	claimed map[shared.ID]time.Time

	// announced is which messages have already been told to their source.
	announced map[shared.ID]bool
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
	// What postgres answers. A fake that returned (nil, nil) here would let the
	// use case take a branch no deployment ever reaches.
	return nil, errs.NotFoundErr("delivery not found").WithErr(delivery.ErrNotFound)
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

// ClaimStale keeps the claims, because that is the behavior the use case now
// depends on: a row this returns must not be returned again until it is released
// or its lease runs out.
func (r *fakeDeliveries) ClaimStale(
	_ context.Context, olderThan, lease time.Duration, limit int,
) ([]delivery.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.claimed == nil {
		r.claimed = map[shared.ID]time.Time{}
	}

	var out []delivery.Delivery
	for _, ds := range r.byNotif {
		for _, d := range ds {
			if d.IsSettled() || r.staleAt.Sub(d.UpdatedAt()) < olderThan {
				continue
			}
			if at, ok := r.claimed[d.ID]; ok && r.staleAt.Sub(at) < lease {
				continue // somebody else holds it
			}

			r.claimed[d.ID] = r.staleAt
			out = append(out, d)
			if len(out) == limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// ClaimAnnouncement is a claim in the fake too, not a counter. A fake that said
// yes every time would let two announcements of one message pass, which is the
// whole thing it exists to prevent.
func (r *fakeDeliveries) ClaimAnnouncement(
	_ context.Context, notificationID shared.ID, _ time.Time,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.announced == nil {
		r.announced = map[shared.ID]bool{}
	}
	if r.announced[notificationID] {
		return false, nil
	}
	r.announced[notificationID] = true
	return true, nil
}

// ids is every delivery this fake holds, in a stable order.
func (r *fakeDeliveries) ids(t *testing.T) []shared.ID {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	var out []shared.ID
	for _, ds := range r.byNotif {
		for i := range ds {
			out = append(out, ds[i].ID)
		}
	}
	slices.Sort(out)
	return out
}

// hold claims a row on somebody else's behalf, so a test can see what a sweep
// does when a delivery is already taken.
func (r *fakeDeliveries) hold(id shared.ID, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.claimed == nil {
		r.claimed = map[shared.ID]time.Time{}
	}
	r.claimed[id] = at
}

func (r *fakeDeliveries) Release(_ context.Context, d *delivery.Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.claimed, d.ID)
	return nil
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

// messages is what it was actually asked to send, so a test can look at what
// reached the provider rather than only how many times.
func (s *fakeSender) messages() []shared.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]shared.Message(nil), s.sent...)
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

func (r *fakeWebhooks) Redirect(_ context.Context, w *webhook.Webhook) error {
	return r.save(w)
}

func (r *fakeWebhooks) RecordSuccess(_ context.Context, w *webhook.Webhook) error {
	return r.save(w)
}

// RecordFailure counts here rather than taking the caller's number, exactly as
// the database does. Nothing else would exercise the path that reads it back.
func (r *fakeWebhooks) RecordFailure(_ context.Context, w *webhook.Webhook) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.bySource[w.SourceID]
	if !ok {
		return 0, errs.NotFoundErr("webhook not found")
	}

	count := stored.ConsecutiveFailures() + 1
	r.bySource[w.SourceID] = webhook.Restore(webhook.Snapshot{
		ID: stored.ID, SourceID: stored.SourceID, CallbackURL: stored.CallbackURL,
		IsActive: stored.IsActive(), ConsecutiveFailures: count,
		CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
	})
	return count, nil
}

func (r *fakeWebhooks) Deactivate(_ context.Context, w *webhook.Webhook) error {
	return r.save(w)
}

func (r *fakeWebhooks) Activate(_ context.Context, w *webhook.Webhook) error {
	return r.save(w)
}

func (r *fakeWebhooks) save(w *webhook.Webhook) error {
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

// --- sign-in ------------------------------------------------------------------

// fakeUsers behaves like postgres in the one way that matters here: an address
// nobody has used comes back as ErrNotFound, never as a nil with no error.
type fakeUsers struct {
	mu   sync.Mutex
	rows map[string]*user.User
}

func newFakeUsers() *fakeUsers { return &fakeUsers{rows: map[string]*user.User{}} }

func (f *fakeUsers) Create(_ context.Context, u *user.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, taken := f.rows[u.Email]; taken {
		return errs.DuplicateErr("that address is already an account")
	}
	f.rows[u.Email] = u
	return nil
}

// ReadByEmail hands back a copy, as postgres would: a row read out of storage,
// not a live handle into it. Returning the stored pointer would let a caller's
// mutation land before any UpdateRole or SetActive call, making the write
// guard around those unobservable.
func (f *fakeUsers) ReadByEmail(_ context.Context, email string) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.rows[email]; ok {
		got := *u
		return &got, nil
	}
	return nil, errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
}

// ReadByID hands back a copy too, for the same reason.
func (f *fakeUsers) ReadByID(_ context.Context, id shared.ID) (*user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.rows {
		if u.ID == id {
			got := *u
			return &got, nil
		}
	}
	return nil, errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
}

func (f *fakeUsers) List(_ context.Context, limit int32) ([]user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]user.User, 0, len(f.rows))
	for _, u := range f.rows {
		out = append(out, *u)
	}
	return capRows(out, limit), nil
}

// UpdateRole writes only what the real statement writes -- role and
// updated_at -- so a use case that carried something else in would leave it
// behind here too, same as fakeSources.UpdateReview.
func (f *fakeUsers) UpdateRole(_ context.Context, u *user.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[u.Email]
	if !ok {
		return errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
	}
	row.Role = u.Role
	row.UpdatedAt = u.UpdatedAt
	return nil
}

// SetActive writes only is_active and updated_at.
func (f *fakeUsers) SetActive(_ context.Context, u *user.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[u.Email]
	if !ok {
		return errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
	}
	row.IsActive = u.IsActive
	row.UpdatedAt = u.UpdatedAt
	return nil
}

type fakeCodes struct {
	mu   sync.Mutex
	rows []*logincode.LoginCode
}

func (f *fakeCodes) Create(_ context.Context, c *logincode.LoginCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, c)
	return nil
}

func (f *fakeCodes) ReadNewest(
	_ context.Context, userID shared.ID,
) (*logincode.LoginCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.rows) - 1; i >= 0; i-- {
		if f.rows[i].UserID == userID {
			return f.rows[i], nil
		}
	}
	return nil, errs.NotFoundErr("no sign-in code").WithErr(logincode.ErrNotFound)
}

// Spend is a no-op because ReadNewest hands back the stored pointer, so Check
// has already written to the row this would update.
func (f *fakeCodes) Spend(_ context.Context, _ *logincode.LoginCode) error { return nil }

// Forget drops the row, the way the repository does. A fake that kept it would
// make TestAFailedSendCostsNoQuota pass against a database that does not.
func (f *fakeCodes) Forget(_ context.Context, id shared.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.rows {
		if c.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *fakeCodes) CountSince(
	_ context.Context, userID shared.ID, since time.Time,
) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.rows {
		if c.UserID == userID && !c.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

type fakeSessions struct {
	mu   sync.Mutex
	rows map[shared.ID]*session.Session
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{rows: map[shared.ID]*session.Session{}}
}

func (f *fakeSessions) Create(_ context.Context, s *session.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[s.ID] = s
	return nil
}

func (f *fakeSessions) Read(_ context.Context, id shared.ID) (*session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.rows[id]; ok {
		return s, nil
	}
	return nil, errs.NotFoundErr("session not found").WithErr(session.ErrNotFound)
}

func (f *fakeSessions) Touch(_ context.Context, _ *session.Session) error { return nil }

func (f *fakeSessions) Delete(_ context.Context, id shared.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[id]; !ok {
		return errs.NotFoundErr("session not found").WithErr(session.ErrNotFound)
	}
	delete(f.rows, id)
	return nil
}

type sentCode struct{ email, code string }

type fakeMailer struct {
	mu   sync.Mutex
	sent []sentCode
	err  error
}

func (f *fakeMailer) SendCode(_ context.Context, email, code string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentCode{email: email, code: code})
	return nil
}
