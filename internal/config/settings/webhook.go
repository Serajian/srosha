package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type Webhook struct {
	// Secrets is one signing secret per source, keyed by source id. A single
	// shared secret would let any source holding it forge a signed callback to
	// another.
	Secrets map[string]env.Secret

	Timeout     time.Duration
	MaxFailures int

	// AllowInsecureURL and AllowPrivateURL relax the callback check for local
	// testing. Production must have both off: they are what stops a source
	// pointing us at our own network.
	AllowInsecureURL bool
	AllowPrivateURL  bool
}

func LoadWebhook(r *env.Reader, production bool) Webhook {
	w := Webhook{
		Secrets:          map[string]env.Secret{},
		Timeout:          r.Duration("WEBHOOK_TIMEOUT", 10*time.Second),
		MaxFailures:      r.Int("WEBHOOK_MAX_FAILURES", 20),
		AllowInsecureURL: r.Bool("WEBHOOK_ALLOW_INSECURE_URL", false),
		AllowPrivateURL:  r.Bool("WEBHOOK_ALLOW_PRIVATE_URL", false),
	}
	r.JSON("WEBHOOK_SECRETS", &w.Secrets)

	r.Check(w.MaxFailures > 0, "NOTIF_WEBHOOK_MAX_FAILURES must be above zero")

	// Getting this wrong on a real deployment turns the callback into a way for
	// a source to reach our private network, so it is refused rather than
	// warned about.
	r.Check(!production || (!w.AllowInsecureURL && !w.AllowPrivateURL),
		"NOTIF_WEBHOOK_ALLOW_INSECURE_URL and NOTIF_WEBHOOK_ALLOW_PRIVATE_URL "+
			"must be off in production")

	return w
}
