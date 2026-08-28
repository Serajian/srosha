// Package user is a person: somebody who signs in. Customers and operators are
// the same kind of row with a different role, which is what keeps one sign-in
// flow instead of two.
package user

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// User is somebody who signs in.
type User struct {
	ID    shared.ID
	Email string
	Role  Role

	// IsActive is whether they may SIGN IN, and nothing else. Whether their
	// sources may send is sources.is_active, and the two are wanted in opposite
	// combinations often enough that neither cascades into the other.
	IsActive bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// New builds a person, lowercasing the address so that two spellings of one
// mailbox are one account.
func New(id shared.ID, email string, role Role, now time.Time) (*User, error) {
	address, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, errs.InvalidInputErr("unknown role").
			WithErr(ErrUnknownRole).
			WithStr(fmt.Sprintf("got %q", role))
	}

	return &User{
		ID:        id,
		Email:     address,
		Role:      role,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// EnsureActive refuses somebody who may not sign in.
func (u *User) EnsureActive() error {
	if u.IsActive {
		return nil
	}
	return errs.ForbiddenErr("this account cannot sign in").
		WithErr(ErrInactive).
		WithStr(fmt.Sprintf("user %q", u.ID))
}

// NormalizeEmail is how an address becomes the thing stored and looked up. It
// is exported because sign-in has to normalize what somebody typed the same way
// before it can find their row.
func NormalizeEmail(email string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(email))
	if t == "" {
		return "", errs.InvalidInputErr("email is required").WithErr(ErrEmptyEmail)
	}
	if len(t) > maxEmailLen {
		return "", errs.InvalidInputErr("email is too long").
			WithErr(ErrInvalidEmail).
			WithStr(fmt.Sprintf("%d chars, max %d", len(t), maxEmailLen))
	}
	if _, err := mail.ParseAddress(t); err != nil {
		return "", errs.InvalidInputErr("email is not an address").WithErr(ErrInvalidEmail)
	}
	return t, nil
}
