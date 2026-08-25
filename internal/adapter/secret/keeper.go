// Package secret keeps a sending identity's secret, sealed, and hands it back
// only at the moment it is used.
//
// The core has nowhere to put a secret -- credential.Credential deliberately
// holds none -- so the entity and its material travel separately and meet here.
// Whether the stored value is encrypted, with which key, and how a key change is
// survived are all questions that stop at this package.
package secret

import (
	"context"
	"log/slog"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/crypto"
	"github.com/Serajian/srosha/pkg/errs"
)

// Store is where sealed material is kept.
//
// Declared here rather than imported, because one adapter never reaches into
// another: the postgres repository satisfies this without knowing that anything
// is encrypted, and could be replaced without this package changing.
type Store interface {
	Create(ctx context.Context, c *credential.Credential, config []byte, secret string) error
	ReadMaterial(ctx context.Context, id shared.ID) (config []byte, secret string, err error)

	// Reseal writes the secret alone, and reports whether the row moved. It
	// matches on the old value, so a reseal that lost a race writes nothing.
	Reseal(ctx context.Context, id shared.ID, previous, secret string, now time.Time) (bool, error)

	// Rotate writes a DIFFERENT secret, which is why it does not match on the
	// old value: a reseal running in between would make that match fail and the
	// rotation would be lost.
	Rotate(ctx context.Context, sourceID string, id shared.ID, secret string, now time.Time) error
}

type Keeper struct {
	store Store
	keys  *crypto.Keyring
	now   shared.NowFunc
	log   *slog.Logger
}

func New(store Store, keys *crypto.Keyring, now shared.NowFunc, log *slog.Logger) (*Keeper, error) {
	switch {
	case store == nil:
		return nil, errs.InternalErr("keeper has nowhere to keep credentials")
	case keys == nil:
		return nil, errs.InternalErr("keeper has no keyring")
	case now == nil:
		return nil, errs.InternalErr("keeper has no clock")
	case log == nil:
		return nil, errs.InternalErr("keeper has no logger")
	}
	return &Keeper{store: store, keys: keys, now: now, log: log}, nil
}

// Add seals the secret and stores the identity with it.
//
// An identity with no secret is a real case -- a relay that needs no password,
// a provider configured entirely by its settings -- and it is stored as nothing
// rather than as the encryption of an empty string. Sealing "" would produce a
// value that looks like a secret to everything downstream.
func (k *Keeper) Add(
	ctx context.Context, c *credential.Credential, config []byte, secret string,
) error {
	sealed := ""
	if secret != "" {
		var err error
		if sealed, err = k.keys.Seal([]byte(secret), bind(c.SourceID, c.Channel, c.ID)); err != nil {
			return err
		}
	}
	return k.store.Create(ctx, c, config, sealed)
}

// Replace seals a new secret over the old one, keeping the identity.
//
// This is what a leaked token needs. Without it a source would have to register
// a second identity under a new name, and every message still naming the old one
// would fail -- turning a token leak into a code change on their side.
func (k *Keeper) Replace(ctx context.Context, c *credential.Credential, secret string) error {
	sealed := ""
	if secret != "" {
		var err error
		if sealed, err = k.keys.Seal([]byte(secret), bind(c.SourceID, c.Channel, c.ID)); err != nil {
			return err
		}
	}
	return k.store.Rotate(ctx, c.SourceID, c.ID, sealed, k.now())
}

// Material opens a credential for one send: the provider settings as stored, and
// the secret in the clear.
//
// sourceID and channel are not looked up -- they are supplied by the caller that
// already resolved this credential, and they are what the seal is bound to. A
// ciphertext copied into another source's row therefore fails to open here,
// without any key having been broken.
func (k *Keeper) Material(
	ctx context.Context, sourceID string, c shared.Channel, id shared.ID,
) (config []byte, secret string, err error) {
	config, sealed, err := k.store.ReadMaterial(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if sealed == "" {
		return config, "", nil
	}

	aad := bind(sourceID, c, id)
	plain, err := k.keys.Open(sealed, aad)
	if err != nil {
		return nil, "", err
	}

	k.reseal(ctx, id, sealed, plain, aad)
	return config, string(plain), nil
}

// reseal is how a key change costs no outage: a value sealed with a key that is
// no longer the active one is written back under the current one, the next time
// anybody reads it. Whatever nobody reads is left to a job.
//
// Best effort, on purpose. The secret is already open and the message is about
// to go out; failing the send because the rewrite failed would turn a tidying
// step into an incident. The next read tries again.
//
// The guard is the whole point: sealing is randomized, so without it every read
// would be followed by a write of a value that was already current.
func (k *Keeper) reseal(ctx context.Context, id shared.ID, sealed string, plain, aad []byte) {
	if !k.keys.NeedsReseal(sealed) {
		return
	}

	next, err := k.keys.Seal(plain, aad)
	if err != nil {
		k.log.WarnContext(ctx, "could not reseal a credential", "credential_id", id, "err", err)
		return
	}

	written, err := k.store.Reseal(ctx, id, sealed, next, k.now())
	if err != nil {
		k.log.WarnContext(ctx, "could not store a resealed credential",
			"credential_id", id, "err", err)
		return
	}
	if written {
		k.log.InfoContext(ctx, "credential resealed under the current key",
			"credential_id", id, "key", k.keys.ActiveID())
	}
}

// bind is what a sealed secret is tied to. Not the name: a name can be changed,
// and a binding that moves is a binding that has to be rewritten.
func bind(sourceID string, c shared.Channel, id shared.ID) []byte {
	return []byte(sourceID + aadSeparator + c.String() + aadSeparator + id.String())
}
