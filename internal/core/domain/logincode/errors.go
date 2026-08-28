package logincode

import "errors"

var (
	ErrNotFound = errors.New("login code not found")

	// ErrWrong is a guess that did not match. It spends the code.
	ErrWrong = errors.New("that code is not right")

	// ErrSpent is a code already used, right or wrong. Ask for another.
	ErrSpent = errors.New("that code has already been used")

	ErrExpired = errors.New("that code has expired")

	// ErrTooManyGuesses is the limit reached. The code is dead.
	ErrTooManyGuesses = errors.New("too many attempts")

	// ErrTooManyRequests is asking for codes faster than the limit allows.
	ErrTooManyRequests = errors.New("too many sign-in requests")
)
