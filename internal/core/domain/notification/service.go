package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

type Service struct {
	repo  Repository
	newID shared.IDFunc
	now   shared.NowFunc

	// keeps is how long this deployment holds a message. It is here because a
	// listing has to be refused against it, and refusing is a rule rather than
	// a query -- the database would answer a question about deleted rows with
	// an empty page and no complaint.
	keeps time.Duration
}

func NewService(
	repo Repository, newID shared.IDFunc, now shared.NowFunc, keeps time.Duration,
) *Service {
	return &Service{repo: repo, newID: newID, now: now, keeps: keeps}
}

// Create builds the message and stores it. Building and storing live together
// so nothing can persist a Notification that never passed New.
func (s *Service) Create(ctx context.Context, org Origin, req Request) (*Notification, error) {
	n, err := New(s.newID(), org, req, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, n); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Service) Get(ctx context.Context, id shared.ID) (*Notification, error) {
	return s.repo.ReadByID(ctx, id)
}

// Page answers "what did I send".
//
// A window that reaches past what this deployment keeps is refused, and the
// answer says how far back it can go. Serving it would return a short page
// that looks complete: the caller cannot tell "you sent nothing then" from
// "we deleted it".
func (s *Service) Page(
	ctx context.Context, sourceID string, w Window, c shared.Cursor,
) (shared.Pagination[Notification], error) {
	if !w.Valid() {
		return shared.Pagination[Notification]{},
			errs.InvalidInputErr("unknown time window").
				WithErr(ErrUnknownWindow).
				WithStr(fmt.Sprintf("window %s", w))
	}
	if w.Length(s.keeps) > s.keeps {
		// The limit is in the message and not only the reason: it is the one
		// thing the caller needs in order to ask again successfully.
		return shared.Pagination[Notification]{},
			errs.InvalidInputErr(fmt.Sprintf(
				"this service keeps messages for %s", humanAge(s.keeps))).
				WithErr(ErrWindowTooLong).
				WithStr(fmt.Sprintf("window %s reaches back %s", w, w.Length(s.keeps)))
	}
	return s.repo.PageBySource(ctx, sourceID, w.Since(s.now(), s.keeps), c)
}

// humanAge renders a retention age the way somebody reading an error thinks of
// it. time.Duration prints 168h0m0s, which is true and no help.
func humanAge(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return d.String()
	}
}

// GetByIdempotencyKey returns nil when the key is unused.
func (s *Service) GetByIdempotencyKey(
	ctx context.Context, sourceID, key string,
) (*Notification, error) {
	return s.repo.ReadByIdempotencyKey(ctx, sourceID, key)
}

// DeleteOlderThan drops one batch of messages older than an age, and says how
// many went. The caller repeats until it stops finding any.
//
// Age alone, and nothing about whether the deliveries settled. A delivery gives
// up at RECONCILE_GIVE_UP, which is minutes; one still pending a month later is
// not work waiting to happen but a row recovery never saw. Config refuses a
// retention age close enough to give-up for that to stop being true.
func (s *Service) DeleteOlderThan(
	ctx context.Context, olderThan time.Duration, batch int,
) (int, error) {
	return s.repo.DeleteOlderThan(ctx, s.now().Add(-olderThan), batch)
}
