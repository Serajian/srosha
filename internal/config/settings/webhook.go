package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// WebhookPolicy is how strict the callback address check is, and it is its own
// group because **both** binaries need it while neither needs the other's half.
//
// The gateway validates an address when a source registers it. The dispatcher
// checks it again after DNS, because a name that passed the first check can
// still resolve onto our own network.
//
// Bundling it with the secrets would mean handing the gateway a map of signing
// keys it has no business holding.
type WebhookPolicy struct {
	// AllowInsecureURL and AllowPrivateURL relax the check for local testing.
	// Production must have both off: they are what stops a source pointing us
	// at our own network.
	AllowInsecureURL bool
	AllowPrivateURL  bool
}

func LoadWebhookPolicy(r *env.Reader, production bool) WebhookPolicy {
	p := WebhookPolicy{
		AllowInsecureURL: r.Bool("WEBHOOK_ALLOW_INSECURE_URL", false),
		AllowPrivateURL:  r.Bool("WEBHOOK_ALLOW_PRIVATE_URL", false),
	}

	// Getting this wrong on a real deployment turns the callback into a way for
	// a source to reach our private network, so it is refused rather than
	// warned about.
	r.Check(!production || (!p.AllowInsecureURL && !p.AllowPrivateURL),
		"NOTIF_WEBHOOK_ALLOW_INSECURE_URL and NOTIF_WEBHOOK_ALLOW_PRIVATE_URL "+
			"must be off in production")

	return p
}

// Webhook is the dispatcher's half: what it needs to make the callback and to
// sign it. The gateway never sees any of this.
type Webhook struct {
	WebhookPolicy

	// Secrets is one signing secret per source, keyed by source id. A single
	// shared secret would let any source holding it forge a signed callback to
	// another.
	Secrets map[string]env.Secret

	Timeout     time.Duration
	MaxFailures int
}

// SecretFor hands over one source's signing secret, and reports whether there
// is one. Reading the map directly would mean passing the whole set to whoever
// signs, and a component that holds every secret is one that can be asked for
// any of them.
func (w Webhook) SecretFor(sourceID string) (string, bool) {
	s, ok := w.Secrets[sourceID]
	if !ok || s.IsZero() {
		return "", false
	}
	return s.Reveal(), true
}

func LoadWebhook(r *env.Reader, production bool) Webhook {
	w := Webhook{
		WebhookPolicy: LoadWebhookPolicy(r, production),
		Secrets:       map[string]env.Secret{},
		Timeout:       r.Duration("WEBHOOK_TIMEOUT", 10*time.Second),
		MaxFailures:   r.Int("WEBHOOK_MAX_FAILURES", 20),
	}
	r.JSON("WEBHOOK_SECRETS", &w.Secrets)

	r.Check(w.MaxFailures > 0, "NOTIF_WEBHOOK_MAX_FAILURES must be above zero")
	return w
}
