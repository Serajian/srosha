package session

import "errors"

var (
	ErrNotFound = errors.New("session not found")

	// ErrClosed is a session past one of its two deadlines.
	ErrClosed = errors.New("session has ended")
)
