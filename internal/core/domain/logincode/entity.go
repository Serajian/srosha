// Package logincode is a one-time code somebody was sent, and what has
// happened to it since.
//
// There are no passwords anywhere near it. That is not only a convenience --
// the first operator has to be written by hand in SQL, and an argon2 hash
// cannot be typed while an email can.
package logincode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// LoginCode is one code somebody was sent, and what has happened to it.
type LoginCode struct {
	ID     shared.ID
	UserID shared.ID

	// Code is stored as it was sent. See the migration for why hashing six
	// digits would be theater.
	Code string

	ExpiresAt time.Time
	Attempts  int

	// UsedAt is set by the first attempt, right or wrong.
	UsedAt *time.Time

	CreatedAt time.Time
}

func New(id, userID shared.ID, code string, now time.Time) *LoginCode {
	return &LoginCode{
		ID:        id,
		UserID:    userID,
		Code:      code,
		ExpiresAt: now.Add(Lifetime),
		CreatedAt: now,
	}
}

// Check answers whether this attempt signs somebody in, and spends the code
// either way.
//
// Spending it on a wrong guess as well as a right one is the point: otherwise a
// wrong answer costs nothing, and the guess limit is all that stands between a
// script and a million tries.
func (c *LoginCode) Check(given string, now time.Time) error {
	switch {
	case c.UsedAt != nil:
		return errs.InvalidInputErr("that code has already been used").WithErr(ErrSpent)
	case c.Attempts >= MaxGuesses:
		return errs.InvalidInputErr("too many attempts").WithErr(ErrTooManyGuesses)
	case !now.Before(c.ExpiresAt):
		return errs.InvalidInputErr("that code has expired").WithErr(ErrExpired)
	}

	c.Attempts++
	spent := now
	c.UsedAt = &spent

	if given != c.Code {
		return errs.InvalidInputErr("that code is not right").WithErr(ErrWrong)
	}
	return nil
}

// Generate makes one. crypto/rand rather than math/rand: this is the whole of
// what stands between an address and an account.
func Generate() (string, error) {
	const space = 1_000_000

	n, err := rand.Int(rand.Reader, big.NewInt(space))
	if err != nil {
		return "", errs.InternalErr("could not generate a sign-in code").WithErr(err)
	}
	return fmt.Sprintf("%0*d", digits, n.Int64()), nil
}
