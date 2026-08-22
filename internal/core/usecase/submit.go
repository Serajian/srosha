// Package usecase drives the domain services: which of them run, in what order,
// and what has to be atomic. Each domain service knows one aggregate; only this
// layer knows the whole story of a request.
package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
)

// UnitOfWork runs everything inside fn in one transaction. This layer decides
// what must be atomic; the adapter decides how, and passes the transaction down
// through ctx so the repositories pick it up.
type UnitOfWork interface {
	Atomically(ctx context.Context, fn func(ctx context.Context) error) error
}

// SubmitCommand is a request to send.
//
// SourceID is filled from the authenticated token, never from the body. Read
// from the body it could be spoofed; here it structurally cannot.
type SubmitCommand struct {
	SourceID string

	IdempotencyKey string
	Title          string
	Body           string
	Priority       shared.Priority
	ExpireAt       *time.Time
	Metadata       map[string]string

	Routes []source.Route

	// Senders names which identity to send from, per channel. Empty means the
	// source's default for that channel.
	Senders map[shared.Channel]string
}

type SubmitResult struct {
	ID                shared.ID
	EffectivePriority shared.Priority

	// Downgraded says the requested priority was above this source's ceiling
	// and was clamped, so the gateway can report it instead of hiding it.
	Downgraded bool

	// Duplicate says this idempotency key had been used, and ID is the original.
	Duplicate bool
}

type Submitter struct {
	sources     *source.Service
	credentials *credential.Service
	notifs      *notification.Service
	deliveries  *delivery.Service
	uow         UnitOfWork
	log         *slog.Logger
}

func NewSubmitter(
	sources *source.Service,
	credentials *credential.Service,
	notifs *notification.Service,
	deliveries *delivery.Service,
	uow UnitOfWork,
	log *slog.Logger,
) *Submitter {
	return &Submitter{
		sources: sources, credentials: credentials,
		notifs: notifs, deliveries: deliveries, uow: uow, log: log,
	}
}

func (s *Submitter) Submit(ctx context.Context, cmd SubmitCommand) (SubmitResult, error) {
	src, err := s.sources.Admit(ctx, cmd.SourceID)
	if err != nil {
		return SubmitResult{}, err
	}

	// Before anything is built: the same key must never produce a second
	// message, however many times a client retries a timed-out request.
	if cmd.IdempotencyKey != "" {
		existing, err := s.notifs.GetByIdempotencyKey(ctx, cmd.SourceID, cmd.IdempotencyKey)
		if err != nil {
			return SubmitResult{}, err
		}
		if existing != nil {
			return duplicateOf(existing), nil
		}
	}

	recipients, err := s.sources.Resolve(src, cmd.Routes)
	if err != nil {
		return SubmitResult{}, err
	}
	if err := s.checkSenders(ctx, cmd); err != nil {
		return SubmitResult{}, err
	}

	var (
		n  *notification.Notification
		ds []delivery.Delivery
	)

	// One transaction: a message written without its deliveries is a message
	// nobody will ever send. Deciding that is this layer's job; how it is done
	// is the adapter's.
	err = s.uow.Atomically(ctx, func(ctx context.Context) error {
		var err error
		n, err = s.notifs.Create(ctx, origin(src), request(cmd))
		if err != nil {
			return err
		}
		ds, err = s.deliveries.Create(ctx, n.ID, recipients, cmd.Senders)
		return err
	})
	// The check above is not enough on its own. A client that timed out and
	// retried puts two requests either side of it, and the second one only finds
	// out at the write. Its transaction rolled back, so nothing of ours was
	// stored: read theirs and answer exactly as the check would have.
	if errors.Is(err, notification.ErrDuplicateKey) {
		return s.raced(ctx, cmd, err)
	}
	if err != nil {
		return SubmitResult{}, err
	}

	s.publish(ctx, n, ds)

	return SubmitResult{
		ID:                n.ID,
		EffectivePriority: n.EffectivePriority,
		Downgraded:        n.WasDowngraded(),
	}, nil
}

// raced answers a key that was taken while we were writing. If the message it
// lost to cannot be read back, the original error is returned rather than a
// guess: something stranger than a race is going on.
func (s *Submitter) raced(
	ctx context.Context, cmd SubmitCommand, cause error,
) (SubmitResult, error) {
	existing, err := s.notifs.GetByIdempotencyKey(ctx, cmd.SourceID, cmd.IdempotencyKey)
	if err != nil || existing == nil {
		return SubmitResult{}, cause
	}
	return duplicateOf(existing), nil
}

// duplicateOf is built in one place so the answer given before the write and the
// answer given after losing the race can never drift apart.
func duplicateOf(n *notification.Notification) SubmitResult {
	return SubmitResult{
		ID:                n.ID,
		EffectivePriority: n.EffectivePriority,
		Downgraded:        n.WasDowngraded(),
		Duplicate:         true,
	}
}

// checkSenders refuses an unknown identity here rather than at send time. A
// typo would otherwise be accepted, then surface hours later as a failed
// delivery the client cannot explain.
func (s *Submitter) checkSenders(ctx context.Context, cmd SubmitCommand) error {
	for c, name := range cmd.Senders {
		if name == "" {
			continue
		}
		if _, err := s.credentials.Resolve(ctx, cmd.SourceID, c, name); err != nil {
			return err
		}
	}
	return nil
}

// publish runs AFTER the commit, and its failures are not the caller's problem.
// The pending delivery row is the record that this must be sent; the sweep
// picks up whatever the broker never heard about.
func (s *Submitter) publish(
	ctx context.Context, n *notification.Notification, ds []delivery.Delivery,
) {
	for _, d := range ds {
		if err := s.deliveries.Publish(ctx, d, n.SourceID, n.EffectivePriority); err != nil {
			s.log.ErrorContext(ctx, "publish failed, leaving it for the sweep",
				"delivery_id", d.ID, "notification_id", n.ID, "err", err)
		}
	}
}

func origin(src *source.Source) notification.Origin {
	return notification.Origin{ID: src.ID, Name: src.Name, MaxPriority: src.MaxPriority}
}

func request(cmd SubmitCommand) notification.Request {
	return notification.Request{
		IdempotencyKey: cmd.IdempotencyKey,
		Title:          cmd.Title,
		Body:           cmd.Body,
		Priority:       cmd.Priority,
		ExpireAt:       cmd.ExpireAt,
		Metadata:       cmd.Metadata,
	}
}
