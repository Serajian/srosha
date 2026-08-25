package notification

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

type Service struct {
	repo  Repository
	newID shared.IDFunc
	now   shared.NowFunc
}

func NewService(repo Repository, newID shared.IDFunc, now shared.NowFunc) *Service {
	return &Service{repo: repo, newID: newID, now: now}
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

// Page answers "what did I send". A window that cannot contain anything is
// refused here rather than asked of the database: it is a question nobody meant
// to ask, and an empty answer would look like an answer.
func (s *Service) Page(
	ctx context.Context, sourceID string, w Window, c shared.Cursor,
) (shared.Pagination[Notification], error) {
	if !w.Valid() {
		return shared.Pagination[Notification]{},
			errs.InvalidInputErr("the time window ends before it starts").WithErr(ErrEmptyWindow)
	}
	return s.repo.PageBySource(ctx, sourceID, w, c)
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
