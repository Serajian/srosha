package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// Alert is where operator alerts go.
//
// A channel of its own, reached directly, and deliberately not srosha's own
// pipeline: an alert that traveled the path it reports on would be silent
// exactly when it matters. See docs/superpowers/specs on operator alerts.
type Alert struct {
	// GotifyURL is the operator's own Gotify. Self-hosted, so there is no host
	// that is right for everybody and none can be a constant here.
	GotifyURL string

	// GotifyToken is an application token, which is what selects the
	// application the message lands in.
	GotifyToken env.Secret

	// Queue is how many alerts may wait before one is dropped. Small on
	// purpose: a backlog of alerts is a backlog of stale news.
	Queue int

	// Timeout bounds one push. Nothing waits on it, so this only decides how
	// long an unreachable server ties up the single worker.
	Timeout time.Duration

	// ReadyEvery is how often a binary asks itself whether its dependencies are
	// still there. Nothing polls readiness otherwise -- /readyz is answered
	// only when somebody asks it.
	ReadyEvery time.Duration

	// DiskFloor is how few free bytes on the filesystem are worth waking
	// somebody for.
	//
	// Free bytes rather than a percentage, because a percentage says nothing on
	// its own: 90% of a 96GB disk is 10GB of room and 90% of a 20GB disk is
	// 2GB, and it is the room that decides whether Postgres can still write.
	//
	// Deliberately far from where the disk sits today. A threshold that fires
	// on the day it is introduced is one people switch off in the same week.
	DiskFloor uint64

	// DiskPath is the mount point to ask about. The root, because the volumes
	// of postgres and nats are on the same filesystem, so asking about it asks
	// about all of them at once.
	DiskPath string

	// DiskEvery is how often to look. Slow on purpose: a disk fills over hours
	// and this is a syscall plus two small queries.
	DiskEvery string
}

// Configured reports whether alerts can be sent at all.
//
// Both, or neither. A half-configured alerter that silently sends nowhere is
// worse than one that is plainly off, because nobody finds out until the day
// they needed it.
func (a Alert) Configured() bool {
	return a.GotifyURL != "" && a.GotifyToken.Reveal() != ""
}

func LoadAlert(r *env.Reader) Alert {
	a := Alert{
		GotifyURL:   r.Str("ALERT_GOTIFY_SERVER_URL", ""),
		GotifyToken: r.Secret("ALERT_GOTIFY_TOKEN", ""),
		Queue:       r.Int("ALERT_QUEUE", 64),
		Timeout:     r.Duration("ALERT_TIMEOUT", 10*time.Second),
		ReadyEvery:  r.Duration("ALERT_READY_EVERY", 30*time.Second),
		DiskFloor:   diskFloor(r.Int("ALERT_DISK_FLOOR_GB", 5)),
		DiskPath:    r.Str("ALERT_DISK_PATH", "/"),
		DiskEvery:   r.Str("ALERT_DISK_EVERY", "@every 15m"),
	}

	r.Check(a.DiskFloor > 0,
		"NOTIF_ALERT_DISK_FLOOR_GB must be above zero: a floor of nothing is "+
			"reached only when the disk is already full, which is too late to "+
			"be told")
	r.Check(a.DiskFloor <= maxDiskFloor,
		"NOTIF_ALERT_DISK_FLOOR_GB is larger than any disk this runs on")
	r.Check(a.DiskPath != "", "NOTIF_ALERT_DISK_PATH must not be empty")
	r.Check(a.DiskEvery != "", "NOTIF_ALERT_DISK_EVERY must not be empty")

	r.Check(a.Queue > 0,
		"NOTIF_ALERT_QUEUE must be above zero: a queue of nothing drops every "+
			"alert, which is not a smaller queue but a switched-off one")

	return a
}

// diskFloor turns gigabytes into bytes without letting a negative one through.
//
// A plain conversion would wrap: -1 becomes the largest uint64 there is, every
// check falls below it, and the alert fires forever. The zero it returns
// instead is caught by the check above, which says what is wrong.
func diskFloor(gb int) uint64 {
	if gb <= 0 || gb > maxDiskFloorGB {
		return 0
	}
	return uint64(gb) << 30
}
