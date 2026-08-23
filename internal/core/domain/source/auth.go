package source

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Authenticator answers "who is calling", and nothing else.
//
// It is separate from Service because it answers a different question with
// different dependencies: Service asks whether a source may send right now,
// this asks whether it is a source at all. Merging them would make every path
// that needs one carry both.
type Authenticator struct {
	keys KeyRepository
	now  shared.NowFunc

	// touchAfter is how stale last_used_at may get. It is operational -- how
	// much write traffic we trade for how fresh the answer is -- so it comes
	// from config rather than living here.
	touchAfter time.Duration
}

func NewAuthenticator(keys KeyRepository, now shared.NowFunc, touchAfter time.Duration) *Authenticator {
	return &Authenticator{keys: keys, now: now, touchAfter: touchAfter}
}

// Authenticate turns a presented key's hash into the source that holds it, and
// returns the key's own id so the caller can record the use.
//
// Every way of failing to find a live key reports the same thing. A revoked
// key, an expired one and one that never existed have to be indistinguishable:
// telling them apart tells whoever is guessing which of their strings was
// once real.
//
// A suspended source is different, and deliberately so. The key is genuine and
// the account is switched off, which is Forbidden rather than Unauthenticated
// -- the customer has to be able to tell "your key is wrong" from "your account
// is off", or they will spend the outage rotating a key that was never the
// problem.
func (a *Authenticator) Authenticate(
	ctx context.Context, keyHash string,
) (*Source, shared.ID, error) {
	if keyHash == "" {
		return nil, "", unknownKey()
	}

	src, keyID, err := a.keys.ReadSourceByKeyHash(ctx, keyHash, a.now())
	if err != nil {
		return nil, "", err
	}
	if src == nil {
		return nil, "", unknownKey()
	}
	if err := src.EnsureActive(); err != nil {
		return nil, "", err
	}
	return src, keyID, nil
}

// RecordUse is deliberately not part of Authenticate. It is bookkeeping, and
// bookkeeping must not be able to refuse a request -- so it is a second call,
// and whoever makes it decides what to do with the error. The only right answer
// is to log it and carry on.
func (a *Authenticator) RecordUse(ctx context.Context, keyID shared.ID) error {
	return a.keys.Touch(ctx, keyID, a.now(), a.touchAfter)
}

// unknownKey is the one answer authentication gives. It is a function rather
// than a value so each call carries its own stack-free envelope and nobody can
// attach a reason to the shared one.
func unknownKey() error {
	return errs.UnauthorizedErr("invalid credentials").WithErr(ErrUnknownKey)
}
