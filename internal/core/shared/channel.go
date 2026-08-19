package shared

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/Serajian/srosha/pkg/errs"
)

// Channel is a delivery route.
//
// Making it a named type rather than a bare string means a typo like
// "telegran" can only enter the system through ParseChannel, which rejects it
// at the boundary. Past that point the compiler guarantees the value is one of
// the four, so no switch downstream needs a "what is this?" branch.
type Channel string

const (
	ChannelEmail    Channel = "email"
	ChannelTelegram Channel = "telegram"
	ChannelBale     Channel = "bale"
	ChannelWhatsApp Channel = "whatsapp"
)

// AllChannels returns the full set in a stable order.
//
// It returns a fresh slice each call rather than exposing a package-level var,
// because a caller sorting or truncating a shared slice would corrupt it for
// everyone else.
func AllChannels() []Channel {
	return []Channel{ChannelEmail, ChannelTelegram, ChannelBale, ChannelWhatsApp}
}

func (c Channel) Valid() bool {
	switch c {
	case ChannelEmail, ChannelTelegram, ChannelBale, ChannelWhatsApp:
		return true
	default:
		return false
	}
}

func (c Channel) String() string { return string(c) }

// ParseChannel validates an untrusted string. Same rule as ParseID: use it on
// input from outside, not on the read path from our own database.
func ParseChannel(s string) (Channel, error) {
	c := Channel(s)
	if !c.Valid() {
		return "", errs.InvalidInputErr("unknown channel").
			WithErr(ErrUnknownChannel).
			WithStr(fmt.Sprintf("got %q", s))
	}
	return c, nil
}

// ValidateTarget checks that a destination has the right SHAPE for this
// channel.
//
// It deliberately does not check existence: whether a mailbox or a chat id is
// real can only be discovered by the sender at delivery time, and a lookup
// here would put a network call inside the domain. The point is to catch an
// email address pasted into a telegram field before it costs a database write,
// a queue round trip and a failed send attempt.
func (c Channel) ValidateTarget(target string) error {
	t := strings.TrimSpace(target)
	if t == "" {
		return errs.InvalidInputErr("delivery target is empty").
			WithErr(ErrEmptyTarget).
			WithStr(fmt.Sprintf("channel %q", c))
	}

	switch c {
	case ChannelEmail:
		if _, err := mail.ParseAddress(t); err != nil {
			return invalidTarget(c, t, "not a valid email address")
		}

	case ChannelTelegram, ChannelBale:
		// Either a numeric chat id -- negative for groups and channels -- or an
		// @username.
		if strings.HasPrefix(t, "@") {
			if len(t) < 2 {
				return invalidTarget(c, t, "empty username")
			}
			return nil
		}
		if !isNumericID(t) {
			return invalidTarget(c, t, "neither a chat id nor an @username")
		}

	case ChannelWhatsApp:
		if !isE164(t) {
			return invalidTarget(c, t, "not an E.164 phone number")
		}

	default:
		// Reached only if a channel constant is added above without a case
		// here. Failing loudly beats silently accepting anything.
		return errs.InvalidInputErr("unknown channel").
			WithErr(ErrUnknownChannel).
			WithStr(fmt.Sprintf("no target rule for %q", c))
	}

	return nil
}

// invalidTarget keeps the client-facing message identical for every failure
// while the reason carries the specifics. The message must not describe our
// accepted formats: that is internal detail, and repeating it back turns the
// API into a probe for how targets are stored.
func invalidTarget(c Channel, target, detail string) error {
	return errs.InvalidInputErr("invalid delivery target").
		WithErr(ErrInvalidTarget).
		WithStr(fmt.Sprintf("channel %q, target %q: %s", c, target, detail))
}

func isNumericID(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isE164 checks the +<digits> form, 8 to 15 digits.
func isE164(s string) bool {
	digits, ok := strings.CutPrefix(s, "+")
	if !ok || len(digits) < 8 || len(digits) > 15 {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
