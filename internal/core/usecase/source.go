package usecase

import (
	"context"
	"fmt"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// SourceRegistration is what a customer may set. What they may not is not in
// here: max_priority, allow_custom_address and is_active are ours, and a form
// that offered them would let a customer raise their own ceiling.
//
// The spec asks for a test that a registration cannot set those. There is none,
// because there is nothing to test: the fields are absent, so a handler that
// tried would not compile. That is a stronger guarantee than a test and it is
// the reason this type exists rather than taking a *source.Source.
type SourceRegistration struct {
	Name             string
	DefaultAddresses map[shared.Channel]string
}

// SourceSettings is what a customer may change afterwards, and it is the same
// three things they set at registration plus the description.
//
// What is absent is the guarantee, exactly as it is for SourceRegistration:
// max_priority, allow_custom_address, is_active, approved_at and owner_user_id
// are not fields here, so a handler that tried to carry one would not compile.
// The statement underneath cannot name them either -- two locks, because this
// is the one the customer's own form posts into.
type SourceSettings struct {
	Name             string
	Description      string
	DefaultAddresses map[shared.Channel]string
}

// Sources is what a customer does with the things they own.
type Sources struct {
	repo  source.Repository
	gate  *Gate
	newID shared.IDFunc
	now   shared.NowFunc
}

func NewSources(
	repo source.Repository, gate *Gate, newID shared.IDFunc, now shared.NowFunc,
) *Sources {
	return &Sources{repo: repo, gate: gate, newID: newID, now: now}
}

// Register creates a source, switched off.
//
// Anybody may register one. Nothing it is given reaches anybody until an
// operator approves it, which is what replaced trying to tell a spammer from a
// customer by what they had configured.
func (s *Sources) Register(
	ctx context.Context, actor *user.User, reg SourceRegistration,
) (*source.Source, error) {
	mine, err := s.repo.ListByOwner(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	if len(mine) >= MaxSourcesPerUser {
		return nil, errs.TooManyErr("you have as many sources as one account may have").
			WithStr(fmt.Sprintf("user %q has %d", actor.ID, len(mine)))
	}

	built, err := source.New(
		s.newID().String(), actor.ID, reg.Name, reg.DefaultAddresses, s.now(),
	)
	if err != nil {
		return nil, err
	}

	act := Act{Verb: ActSourceCreate, TargetType: "source", TargetID: built.ID}
	err = s.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return s.repo.Create(ctx, built)
	})
	if err != nil {
		return nil, err
	}
	return built, nil
}

// Mine is everything this person registered.
func (s *Sources) Mine(ctx context.Context, actor *user.User) ([]source.Source, error) {
	return s.repo.ListByOwner(ctx, actor.ID)
}

// One is a source this person owns.
//
// Somebody else's answers ErrNotFound rather than a refusal, deliberately: a
// refusal would confirm that the id exists, and an id is guessable in a way a
// source's contents are not.
func (s *Sources) One(
	ctx context.Context, actor *user.User, id string,
) (*source.Source, error) {
	src, err := s.repo.ReadByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if src.OwnerUserID != actor.ID {
		return nil, errs.NotFoundErr("no such source").
			WithErr(source.ErrNotFound).
			WithStr(fmt.Sprintf("user %q asked for source %q, owned by %q",
				actor.ID, id, src.OwnerUserID))
	}
	return src, nil
}

// Update changes the three things a customer owns on a source they own.
//
// Ownership is checked by reading through One, which answers ErrNotFound for
// somebody else's source rather than refusing it -- a refusal would confirm the
// id exists.
//
// Keys, senders and callbacks are not touched. They are their own resources
// with their own pages and their own audit verbs, and folding them in here
// would mean a rename could revoke a key.
func (s *Sources) Update(
	ctx context.Context, actor *user.User, id string, set SourceSettings,
) (*source.Source, error) {
	src, err := s.One(ctx, actor, id)
	if err != nil {
		return nil, err
	}

	if err := src.Reconfigure(set.Name, set.Description, set.DefaultAddresses, s.now()); err != nil {
		return nil, err
	}

	act := Act{Verb: ActSourceUpdate, TargetType: "source", TargetID: src.ID}
	err = s.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return s.repo.UpdateSettings(ctx, src)
	})
	if err != nil {
		return nil, err
	}
	return src, nil
}
