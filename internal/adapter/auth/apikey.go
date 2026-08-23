// Package auth is the only place that knows what an API key looks like and how
// it is stored. Nothing else -- not the core, not the database adapter -- ever
// sees a key in the clear except the request it arrived on.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/pkg/errs"
)

// Scheme is what an API key of ours is: how one is made, how it is stored, and
// how one is recognized. The word is the same one HTTP uses for Bearer -- this
// is the scheme, not the keys, which live in a table.
//
// It holds nothing -- a key's shape is fixed, not configured -- and is a type
// anyway, so that whoever needs it is handed one rather than reaching for this
// package by name. Every other adapter here is built in bootstrap and passed
// down; this is not the place to be the exception, and a caller that takes it
// as a field says in its own signature what it depends on.
//
// A value rather than a pointer: there is no state to share and no nil to
// guard against.
type Scheme struct{}

func NewScheme() Scheme { return Scheme{} }

// Mint produces a key to hand to a customer, and the hash to store.
//
// The key is returned exactly once. We keep only the hash, so there is nothing
// to show a second time -- which is the point: a database dump does not hand
// anybody a working key.
func (s Scheme) Mint() (key, hash string, err error) {
	raw := make([]byte, randomBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", errs.InternalErr("could not generate a key").WithErr(err)
	}

	key = prefix + base64.RawURLEncoding.EncodeToString(raw)
	return key, s.Hash(key), nil
}

// Hash is what a presented key becomes before it is looked up.
//
// SHA-256, not bcrypt or argon2. Those exist to make guessing a low-entropy
// human password expensive, and their per-row salt would turn this lookup into
// a full scan -- there is no username to find the row by first, so the hash
// itself has to be the thing indexed. A 256-bit random key is not guessable in
// the first place, so there is nothing for the slowness to buy.
//
// The whole presented string is hashed, prefix included: what the customer
// pastes is what we hash.
func (Scheme) Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Parse checks the shape before anything touches the database, so a garbage
// header costs no query at all.
//
// It reports the same error as an unknown key, and must keep doing so. A
// separate "malformed" answer would let somebody learn our format by watching
// which of their guesses is answered differently.
func (s Scheme) Parse(presented string) (string, error) {
	body, ok := strings.CutPrefix(presented, prefix)
	if !ok || len(body) != bodyLen {
		return "", refused()
	}
	if _, err := base64.RawURLEncoding.DecodeString(body); err != nil {
		return "", refused()
	}
	return s.Hash(presented), nil
}

// refused is the one answer a key of the wrong shape gets, and it is the same
// one an unknown key gets from the core. Two different answers would turn the
// API into a probe for our format.
func refused() error {
	return errs.UnauthorizedErr("invalid credentials").WithErr(source.ErrUnknownKey)
}
