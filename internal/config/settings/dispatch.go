package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type Dispatch struct {
	// MaxAttempts must match the broker's own delivery limit. Set it higher and
	// the broker gives up first, leaving the row pending with no outcome on it.
	MaxAttempts int

	// ReconcileAfter is how long a delivery may sit pending before recovery
	// picks it up; ReconcileGiveUp is the age past which its next attempt is
	// its last. The gap between them decides how many attempts a row gets.
	ReconcileAfter  time.Duration
	ReconcileGiveUp time.Duration
	ReconcileBatch  int
	ReconcileEvery  time.Duration
}

func LoadDispatch(r *env.Reader) Dispatch {
	d := Dispatch{
		MaxAttempts:     r.Int("DISPATCH_MAX_ATTEMPTS", 5),
		ReconcileAfter:  r.Duration("RECONCILE_AFTER", 5*time.Minute),
		ReconcileGiveUp: r.Duration("RECONCILE_GIVE_UP", 30*time.Minute),
		ReconcileBatch:  r.Int("RECONCILE_BATCH", 100),
		ReconcileEvery:  r.Duration("RECONCILE_EVERY", 5*time.Minute),
	}

	r.Check(d.MaxAttempts > 0, "NOTIF_DISPATCH_MAX_ATTEMPTS must be above zero")
	r.Check(d.ReconcileBatch > 0, "NOTIF_RECONCILE_BATCH must be above zero")

	// With give-up at or below after, a row is already past its last chance the
	// first time recovery sees it, so it never gets a second attempt.
	r.Check(d.ReconcileGiveUp > d.ReconcileAfter,
		"NOTIF_RECONCILE_GIVE_UP must be longer than NOTIF_RECONCILE_AFTER")

	return d
}
