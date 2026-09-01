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
	}

	r.Check(a.Queue > 0,
		"NOTIF_ALERT_QUEUE must be above zero: a queue of nothing drops every "+
			"alert, which is not a smaller queue but a switched-off one")

	return a
}
