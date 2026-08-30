package user

import "errors"

var (
	ErrNotFound     = errors.New("user not found")
	ErrEmptyEmail   = errors.New("email is required")
	ErrInvalidEmail = errors.New("email is not an address")
	ErrUnknownRole  = errors.New("unknown role")

	// ErrInactive is refused where somebody tries to SIGN IN. A source that may
	// not send is a different question with a different answer.
	ErrInactive = errors.New("user cannot sign in")
)
