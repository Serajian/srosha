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
	// ReconcileSchedule is when recovery sweeps, as a cron spec or an interval
	// descriptor: "@every 5m", "*/5 * * * *", "0 3 * * *". A string rather than
	// a duration because one parser reads all three, so a deployment that
	// outgrows an interval needs no new setting.
	ReconcileSchedule string

	// AckWait is how long the broker waits for an acknowledgement before it
	// decides nobody handled the message and gives it to somebody else. It has
	// to be longer than the slowest send: shorter, and a provider that takes
	// its time gets the same message twice.
	AckWait time.Duration

	// MaxInFlight bounds how many deliveries are being worked on at once, which
	// is this binary's concurrency. Unbounded, a full queue would open hundreds
	// of connections to one provider at the same moment.
	MaxInFlight int
}

func LoadDispatch(r *env.Reader) Dispatch {
	d := Dispatch{
		MaxAttempts:       r.Int("DISPATCH_MAX_ATTEMPTS", 5),
		ReconcileAfter:    r.Duration("RECONCILE_AFTER", 5*time.Minute),
		ReconcileGiveUp:   r.Duration("RECONCILE_GIVE_UP", 30*time.Minute),
		ReconcileBatch:    r.Int("RECONCILE_BATCH", 100),
		ReconcileSchedule: r.Str("RECONCILE_SCHEDULE", "@every 5m"),
		AckWait:           r.Duration("DISPATCH_ACK_WAIT", time.Minute),
		MaxInFlight:       r.Int("DISPATCH_MAX_IN_FLIGHT", 50),
	}

	r.Check(d.MaxAttempts > 0, "NOTIF_DISPATCH_MAX_ATTEMPTS must be above zero")
	r.Check(d.ReconcileBatch > 0, "NOTIF_RECONCILE_BATCH must be above zero")
	// The spec itself is checked by the scheduler, which owns the parser.
	r.Check(d.ReconcileSchedule != "", "NOTIF_RECONCILE_SCHEDULE must not be empty")
	r.Check(d.MaxInFlight > 0, "NOTIF_DISPATCH_MAX_IN_FLIGHT must be above zero")

	// Zero would leave the broker's own default in place, which is a number
	// nobody here chose and which may well be under a slow provider's timeout.
	r.Check(d.AckWait > 0, "NOTIF_DISPATCH_ACK_WAIT must be above zero")

	// With give-up at or below after, a row is already past its last chance the
	// first time recovery sees it, so it never gets a second attempt.
	r.Check(d.ReconcileGiveUp > d.ReconcileAfter,
		"NOTIF_RECONCILE_GIVE_UP must be longer than NOTIF_RECONCILE_AFTER")

	return d
}
