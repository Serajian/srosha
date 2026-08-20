package webhook

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Service struct {
	repo   Repository
	newID  shared.IDFunc
	now    shared.NowFunc
	policy URLPolicy
}

func NewService(repo Repository, newID shared.IDFunc, now shared.NowFunc, p URLPolicy) *Service {
	return &Service{repo: repo, newID: newID, now: now, policy: p}
}

// Register sets a source's callback. One source has one webhook, so registering
// again changes the existing one rather than adding a second.
//
// A new address gets a clean start: switched on, failure run cleared. If we had
// switched it off because the old endpoint was dead, that says nothing about
// the new one.
func (s *Service) Register(
	ctx context.Context, sourceID string, r Registration,
) (*Webhook, error) {
	existing, err := s.repo.ReadBySourceID(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		w, err := New(s.newID(), sourceID, r, s.policy, s.now())
		if err != nil {
			return nil, err
		}
		if err := s.repo.Create(ctx, w); err != nil {
			return nil, err
		}
		return w, nil
	}

	if err := existing.Redirect(r, s.policy, s.now()); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *Service) Get(ctx context.Context, sourceID string) (*Webhook, error) {
	return s.repo.ReadBySourceID(ctx, sourceID)
}

func (s *Service) RecordSuccess(ctx context.Context, w *Webhook) error {
	w.RecordSuccess(s.now())
	return s.repo.Update(ctx, w)
}

func (s *Service) RecordFailure(ctx context.Context, w *Webhook, maxFailures int) error {
	w.RecordFailure(maxFailures, s.now())
	return s.repo.Update(ctx, w)
}

func (s *Service) Deactivate(ctx context.Context, w *Webhook) error {
	w.Deactivate(s.now())
	return s.repo.Update(ctx, w)
}
