// Package webhook holds where a source wants delivery outcomes pushed to.
package webhook

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Webhook is a source's callback: one per source.
//
// The signing secret is not here. Only the dispatcher needs it, and only while
// signing; the gateway loads this to register and list.
type Webhook struct {
	ID       shared.ID
	SourceID string

	CallbackURL string

	CreatedAt time.Time
	UpdatedAt time.Time

	isActive            bool
	consecutiveFailures int
}

func New(
	id shared.ID, sourceID string, r Registration, p URLPolicy, now time.Time,
) (*Webhook, error) {
	if id.IsZero() {
		return nil, errs.InternalErr("webhook id is missing").WithErr(shared.ErrInvalidID)
	}
	if sourceID == "" {
		return nil, errs.InternalErr("source is missing").WithErr(ErrMissingSource)
	}
	if now.IsZero() {
		return nil, errs.InternalErr("creation timestamp is missing").WithErr(shared.ErrInvalidID)
	}
	if err := validateURL(r.CallbackURL, p); err != nil {
		return nil, err
	}

	return &Webhook{
		ID:          id,
		SourceID:    sourceID,
		CallbackURL: r.CallbackURL,
		CreatedAt:   now,
		UpdatedAt:   now,
		isActive:    true,
	}, nil
}

// Restore rebuilds from storage WITHOUT validation. Repository only.
func Restore(s Snapshot) *Webhook {
	return &Webhook{
		ID:                  s.ID,
		SourceID:            s.SourceID,
		CallbackURL:         s.CallbackURL,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
		isActive:            s.IsActive,
		consecutiveFailures: s.ConsecutiveFailures,
	}
}

// Redirect points an existing webhook somewhere else. It validates exactly as
// New does -- an address that would be refused today must not slip in through
// an update -- and clears the failure run, because a new address has not failed
// at anything yet.
func (w *Webhook) Redirect(r Registration, p URLPolicy, now time.Time) error {
	if err := validateURL(r.CallbackURL, p); err != nil {
		return err
	}
	w.CallbackURL = r.CallbackURL
	w.isActive = true
	w.consecutiveFailures = 0
	w.UpdatedAt = now
	return nil
}

func (w *Webhook) IsActive() bool           { return w.isActive }
func (w *Webhook) ConsecutiveFailures() int { return w.consecutiveFailures }

// RecordSuccess clears the failure run, so an endpoint that fails now and then
// is never switched off.
func (w *Webhook) RecordSuccess(now time.Time) {
	w.consecutiveFailures = 0
	w.UpdatedAt = now
}

// RecordFailure switches the webhook off once the run reaches maxFailures, so a
// dead endpoint is not hammered forever. The limit is operational, so it comes
// from config rather than living here.
func (w *Webhook) RecordFailure(maxFailures int, now time.Time) {
	w.consecutiveFailures++
	w.UpdatedAt = now
	if maxFailures > 0 && w.consecutiveFailures >= maxFailures {
		w.isActive = false
	}
}

func (w *Webhook) Deactivate(now time.Time) {
	w.isActive = false
	w.UpdatedAt = now
}

// Activate clears the failure run too: switching it back on without that would
// deactivate it again on the first hiccup.
func (w *Webhook) Activate(now time.Time) {
	w.isActive = true
	w.consecutiveFailures = 0
	w.UpdatedAt = now
}

// validateURL is a security control, not a formatting check. A callback makes
// US call an address THEY chose, so without this a source can reach anything on
// our private network -- the unauthenticated NATS monitoring port, the cloud
// metadata endpoint, the database.
//
// Shape only. A name that resolves to a private address passes here and must be
// checked again after DNS, at request time.
func validateURL(raw string, p URLPolicy) error {
	if strings.TrimSpace(raw) == "" {
		return errs.InvalidInputErr("callback url is required").WithErr(ErrEmptyURL)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return errs.InvalidInputErr("callback url is not valid").
			WithErr(ErrMalformedURL).
			WithStr(fmt.Sprintf("url %q: %v", raw, err))
	}
	if u.Host == "" {
		return errs.InvalidInputErr("callback url is not valid").
			WithErr(ErrMalformedURL).
			WithStr(fmt.Sprintf("url %q has no host", raw))
	}
	if u.User != nil {
		return errs.InvalidInputErr("callback url must not carry credentials").
			WithErr(ErrCredentialsInURL)
	}

	switch u.Scheme {
	case "https":
	case "http":
		if !p.AllowInsecure {
			return errs.InvalidInputErr("callback url must use https").
				WithErr(ErrInsecureURL)
		}
	default:
		return errs.InvalidInputErr("callback url must use https").
			WithErr(ErrInsecureURL).
			WithStr(fmt.Sprintf("scheme %q", u.Scheme))
	}

	if !p.AllowPrivate && isPrivateHost(u.Hostname()) {
		return errs.InvalidInputErr("callback url is not reachable").
			WithErr(ErrPrivateURL).
			WithStr(fmt.Sprintf("host %q", u.Hostname()))
	}
	return nil
}

func isPrivateHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))

	if addr, err := netip.ParseAddr(h); err == nil {
		return addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
			addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast()
	}

	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") {
		return true
	}
	// A name with no dot is a container or service name on our own network:
	// "nats", "postgres". A real callback is always a fully qualified name.
	return !strings.Contains(h, ".")
}
