package shared

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"strconv"
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
	ChannelMatrix   Channel = "matrix"
	ChannelGotify   Channel = "gotify"
	ChannelFCM      Channel = "fcm"
	ChannelAPNs     Channel = "apns"
)

// AllChannels returns the full set in a stable order.
//
// It returns a fresh slice each call rather than exposing a package-level var,
// because a caller sorting or truncating a shared slice would corrupt it for
// everyone else.
func AllChannels() []Channel {
	return []Channel{
		ChannelEmail, ChannelTelegram, ChannelBale, ChannelWhatsApp,
		ChannelMatrix, ChannelGotify, ChannelFCM, ChannelAPNs,
	}
}

func (c Channel) Valid() bool {
	switch c {
	case ChannelEmail, ChannelTelegram, ChannelBale, ChannelWhatsApp,
		ChannelMatrix, ChannelGotify, ChannelFCM, ChannelAPNs:
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

// ValidateAddress checks that a destination has the right SHAPE for this
// channel.
//
// It deliberately does not check existence: whether a mailbox or a chat id is
// real can only be discovered by the sender at delivery time, and a lookup
// here would put a network call inside the domain. The point is to catch an
// email address pasted into a telegram field before it costs a database write,
// a queue round trip and a failed send attempt.
func (c Channel) ValidateAddress(address string) error {
	t := strings.TrimSpace(address)
	if t == "" {
		return errs.InvalidInputErr("delivery address is empty").
			WithErr(ErrEmptyAddress).
			WithStr(fmt.Sprintf("channel %q", c))
	}

	switch c {
	case ChannelEmail:
		if _, err := mail.ParseAddress(t); err != nil {
			return invalidAddress(c, t, "not a valid email address")
		}

	case ChannelTelegram, ChannelBale:
		// A numeric chat id, negative for groups and channels. Or an @name,
		// which the Bot API resolves only for PUBLIC channels -- never for a
		// person, whatever their username. The two are indistinguishable here,
		// so the shape is all we can check.
		if strings.HasPrefix(t, "@") {
			if !isUsername(t[1:]) {
				return invalidAddress(c, t, "not a valid @name")
			}
			return nil
		}
		if !isChatID(t) {
			return invalidAddress(c, t, "neither a chat id nor an @name")
		}

	case ChannelWhatsApp:
		if !isE164(t) {
			return invalidAddress(c, t, "not an E.164 phone number")
		}

	case ChannelFCM:
		// A device token, and there is no rule to check it against: Google
		// issues them, has changed their length between versions and promises
		// nothing about their characters. A shape invented here would one day
		// refuse a token that works, so only the obviously-wrong is refused.
		if len(t) < minDeviceTokenLen || len(t) > maxDeviceTokenLen {
			return invalidAddress(c, t, "not a device token")
		}

	case ChannelAPNs:
		// Hexadecimal, unlike FCM's, and checked -- because this one becomes
		// part of a url rather than a value in a json body. The length is a
		// range because Apple's have been 64 characters for a long time
		// without that ever being promised.
		if len(t) < minAPNsTokenLen || len(t) > maxAPNsTokenLen || !isHex(t) {
			return invalidAddress(c, t, "not a device token")
		}

	case ChannelMatrix:
		// A room, and only a room. Matrix has no "send to this person": you
		// send to a room, and reaching a person means finding or creating a
		// private one with them -- conversation state this service does not
		// keep. A user id here would be accepted and then fail on every send,
		// so it is refused where it can still be reported as a mistake.
		if !isMatrixRoom(t) {
			return invalidAddress(c, t, "not a matrix room id")
		}

	case ChannelGotify:
		// The application id Gotify assigns when an application is created:
		// its own primary key, a positive integer starting at 1 -- based on
		// Gotify's documented data model, not verified against a live server.
		// There is no other documented shape for it, so only that much is
		// checked; a rule invented past this would one day refuse an id that
		// works.
		if !isGotifyAppID(t) {
			return invalidAddress(c, t, "not a gotify application id")
		}

	default:
		// Reached only if a channel constant is added above without a case
		// here. Failing loudly beats silently accepting anything.
		return errs.InvalidInputErr("unknown channel").
			WithErr(ErrUnknownChannel).
			WithStr(fmt.Sprintf("no address rule for %q", c))
	}

	return nil
}

// invalidAddress keeps the client-facing message identical for every failure
// while the reason carries the specifics. The message must not describe our
// accepted formats: that is internal detail, and repeating it back turns the
// API into a probe for how addresses are stored.
func invalidAddress(c Channel, address, detail string) error {
	return errs.InvalidInputErr("invalid delivery address").
		WithErr(ErrInvalidAddress).
		WithStr(fmt.Sprintf("channel %q, address %q: %s", c, address, detail))
}

// isChatID checks the value fits an int64, which is what a chat id is. Digits
// alone are not enough: a forty-digit number is not an id, it is a typo.
func isChatID(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}

// isUsername follows the Telegram rule: 5 to 32 characters, letters, digits and
// underscores, starting with a letter and ending with a letter or digit.
func isUsername(s string) bool {
	if len(s) < minUsernameLen || len(s) > maxUsernameLen {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		case r == '_':
			if i == 0 || i == len(s)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isMatrixRoom checks the shape of a room id: an exclamation mark, an opaque
// local part, a colon and the server that issued it.
//
// The local part is not checked beyond being there: it is the homeserver's to
// choose and nothing about it is ours to have an opinion on.
func isMatrixRoom(s string) bool {
	local, ok := strings.CutPrefix(s, matrixRoomSigil)
	if !ok {
		return false
	}

	name, server, found := strings.Cut(local, ":")
	return found && name != "" && server != ""
}

// isGotifyAppID checks the value fits a positive integer, which is what
// Gotify's own application id is: an auto-incrementing primary key starting
// at 1. Unlike isChatID, negative and zero are refused -- there is no
// application numbered that way.
func isGotifyAppID(s string) bool {
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n > 0
}

func isE164(s string) bool {
	digits, ok := strings.CutPrefix(s, "+")
	if !ok || len(digits) < minE164Digits || len(digits) > maxE164Digits {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// MarshalJSON and UnmarshalJSON keep an unknown channel from crossing a wire in
// either direction. Without them "carrier-pigeon" decodes quietly into a
// Channel that every switch downstream has to guess at.
func (c Channel) MarshalJSON() ([]byte, error) {
	if !c.Valid() {
		return nil, errs.InternalErr("unknown channel").
			WithErr(ErrUnknownChannel).
			WithStr(fmt.Sprintf("got %q", string(c)))
	}
	return json.Marshal(string(c))
}

func (c *Channel) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return errs.InvalidInputErr("unknown channel").
			WithErr(ErrUnknownChannel).
			WithStr(fmt.Sprintf("got %s", b))
	}

	parsed, err := ParseChannel(name)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// isHex reports whether every character is a hexadecimal digit. An APNs device
// token is written that way, and it ends up in a url path.
func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
