package session

import "time"

// Lifetime is how long one sign-in lasts however busy it is, and IdleTimeout is
// how long an abandoned browser stays open. Two deadlines, because they answer
// different questions: the first bounds a stolen cookie, the second an unlocked
// laptop.
const (
	Lifetime    = 12 * time.Hour
	IdleTimeout = 2 * time.Hour
)
