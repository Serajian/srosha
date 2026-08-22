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
	if err := s.repo.Redirect(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *Service) Get(ctx context.Context, sourceID string) (*Webhook, error) {
	return s.repo.ReadBySourceID(ctx, sourceID)
}

func (s *Service) RecordSuccess(ctx context.Context, w *Webhook) error {
	w.RecordSuccess(s.now())
	return s.repo.RecordSuccess(ctx, w)
}

// RecordFailure counts first and judges second: storage owns the number,
// because callbacks for one source settle at once and each holds its own copy
// of this entity, and the limit stays here, because it is a rule with tests.
//
// The second write happens only on the run that switches the webhook off.
func (s *Service) RecordFailure(ctx context.Context, w *Webhook, maxFailures int) error {
	count, err := s.repo.RecordFailure(ctx, w)
	if err != nil {
		return err
	}

	wasActive := w.IsActive()
	w.RecordFailure(count, maxFailures, s.now())
	if wasActive && !w.IsActive() {
		return s.repo.Deactivate(ctx, w)
	}
	return nil
}

func (s *Service) Deactivate(ctx context.Context, w *Webhook) error {
	w.Deactivate(s.now())
	return s.repo.Deactivate(ctx, w)
}

// Activate is the way back from Deactivate, and clears the failure run so a
// callback switched off for being dead is not switched off again by the first
// hiccup after it was fixed.
func (s *Service) Activate(ctx context.Context, w *Webhook) error {
	w.Activate(s.now())
	return s.repo.Activate(ctx, w)
}
