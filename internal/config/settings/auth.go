package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type Auth struct {
	// KeyTouchAfter is how stale last_used_at may get. Every authenticated
	// request would otherwise carry an UPDATE, and the question the column
	// exists to answer -- when was this key last used -- does not need the
	// answer to the minute.
	KeyTouchAfter time.Duration
}

func LoadAuth(r *env.Reader) Auth {
	a := Auth{KeyTouchAfter: r.Duration("AUTH_KEY_TOUCH_AFTER", time.Hour)}
	// Zero would mean an UPDATE on every request, which is the thing the window
	// exists to prevent.
	r.Check(a.KeyTouchAfter > 0, "NOTIF_AUTH_KEY_TOUCH_AFTER must be above zero")
	return a
}
