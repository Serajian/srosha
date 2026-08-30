package session_test

import (
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/shared"
)

var (
	sessionID = shared.ID("01K0SESS0000000000000000AB")
	userID    = shared.ID("01K0ACCT0000000000000000AB")
	now       = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
)

func TestAFreshSessionIsValid(t *testing.T) {
	s := session.New(sessionID, userID, now)

	if !s.Valid(now.Add(time.Minute)) {
		t.Error("a session a minute old was refused")
	}
}

// Two deadlines, and they answer different questions: one bounds how long a
// sign-in lasts at all, the other how long an abandoned browser stays open.
func TestASessionEndsAtItsAbsoluteDeadline(t *testing.T) {
	s := session.New(sessionID, userID, now)

	// Busy the whole time -- and it still ends.
	at := now
	for at.Before(now.Add(session.Lifetime)) {
		at = at.Add(time.Minute)
		s.Touch(at)
	}

	if s.Valid(now.Add(session.Lifetime + time.Second)) {
		t.Error("a session outlived its absolute deadline")
	}
}

func TestASessionEndsWhenItIsLeftAlone(t *testing.T) {
	s := session.New(sessionID, userID, now)

	idle := now.Add(session.IdleTimeout + time.Second)
	if s.Valid(idle) {
		t.Error("an idle session was still valid")
	}
}

func TestTouchKeepsItAlive(t *testing.T) {
	s := session.New(sessionID, userID, now)

	almost := now.Add(session.IdleTimeout - time.Minute)
	s.Touch(almost)

	if !s.Valid(almost.Add(time.Minute)) {
		t.Error("a touched session went idle anyway")
	}
}
