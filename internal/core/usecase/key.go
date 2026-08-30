package usecase

import (
	"context"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// KeyMinter makes a key and the hash it is found by.
//
// Declared here rather than imported: what a key looks like and how it is
// hashed is the adapter's business, and this layer only knows that one call
// produces both and that only the hash is kept.
type KeyMinter interface {
	Mint() (key, hash string, err error)
}

// Keys is issuing, listing and revoking a source's keys.
type Keys struct {
	issuer  source.KeyIssuer
	sources *Sources
	minter  KeyMinter
	gate    *Gate
	newID   shared.IDFunc
	now     shared.NowFunc
}

func NewKeys(
	issuer source.KeyIssuer, sources *Sources, minter KeyMinter,
	gate *Gate, newID shared.IDFunc, now shared.NowFunc,
) *Keys {
	return &Keys{
		issuer:  issuer,
		sources: sources,
		minter:  minter,
		gate:    gate,
		newID:   newID,
		now:     now,
	}
}

// Issue makes a key and hands it back once.
//
// The key is returned and never stored: what goes in the row is the hash it
// will be looked up by. There is no second chance to read it, which is why the
// page that calls this says so before the customer navigates away.
func (k *Keys) Issue(
	ctx context.Context, actor *user.User, sourceID, label string,
) (string, *source.Key, error) {
	if _, err := k.sources.One(ctx, actor, sourceID); err != nil {
		return "", nil, err
	}

	key, hash, err := k.minter.Mint()
	if err != nil {
		return "", nil, err
	}

	built := &source.Key{
		ID:        k.newID(),
		SourceID:  sourceID,
		Label:     label,
		CreatedAt: k.now(),
	}

	act := Act{Verb: ActKeyIssue, TargetType: "key", TargetID: built.ID.String()}
	err = k.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return k.issuer.Create(ctx, built, hash)
	})
	if err != nil {
		return "", nil, err
	}
	return key, built, nil
}

// List is a source's keys. The keys themselves are not in it and cannot be:
// only their hashes were kept.
func (k *Keys) List(
	ctx context.Context, actor *user.User, sourceID string,
) ([]source.Key, error) {
	if _, err := k.sources.One(ctx, actor, sourceID); err != nil {
		return nil, err
	}
	return k.issuer.ListBySourceID(ctx, sourceID)
}

// Revoke marks a key, and never deletes it. After an incident the questions are
// when it was revoked and when it was last used, and a deleted row answers
// neither.
func (k *Keys) Revoke(
	ctx context.Context, actor *user.User, sourceID string, keyID shared.ID,
) error {
	if _, err := k.sources.One(ctx, actor, sourceID); err != nil {
		return err
	}

	keys, err := k.issuer.ListBySourceID(ctx, sourceID)
	if err != nil {
		return err
	}
	if !holds(keys, keyID) {
		return errs.NotFoundErr("no such key").WithErr(source.ErrKeyNotFound)
	}

	act := Act{Verb: ActKeyRevoke, TargetType: "key", TargetID: keyID.String()}
	return k.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return k.issuer.Revoke(ctx, keyID, k.now())
	})
}

// holds is why Revoke lists first: a key id belongs to a source, and taking the
// caller's word for which one would let somebody revoke a key on a source they
// do not own by naming it.
func holds(keys []source.Key, id shared.ID) bool {
	for i := range keys {
		if keys[i].ID == id {
			return true
		}
	}
	return false
}
