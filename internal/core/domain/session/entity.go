// Package session is a signed-in browser.
//
// It is kept server-side rather than only in a signed cookie, so that
// deactivating somebody ends their session on the next request. A
// self-contained token would keep working until it expired, which is the wrong
// answer to "this person left".
package session

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Session is a signed-in browser.
//
// Whether the person behind it may still sign in is not asked here: that is
// user.EnsureActive, checked on every request against the row rather than
// against anything carried in the session.
type Session struct {
	ID     shared.ID
	UserID shared.ID

	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
}

func New(id, userID shared.ID, now time.Time) *Session {
	return &Session{
		ID:         id,
		UserID:     userID,
		ExpiresAt:  now.Add(Lifetime),
		LastSeenAt: now,
		CreatedAt:  now,
	}
}

// Valid reports whether this session is still open: inside its absolute
// deadline, and used recently enough.
func (s *Session) Valid(now time.Time) bool {
	if !now.Before(s.ExpiresAt) {
		return false
	}
	return now.Sub(s.LastSeenAt) < IdleTimeout
}

// Touch moves the idle deadline. It does not move the absolute one.
func (s *Session) Touch(now time.Time) { s.LastSeenAt = now }
