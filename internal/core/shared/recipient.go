package shared

import (
	"fmt"

	"github.com/Serajian/srosha/pkg/errs"
)

// Recipient is one destination: a channel and the address inside it. Neither
// means anything alone -- "12345" is a chat id or a phone number depending on
// the channel. Comparable on purpose, so duplicate detection is a map lookup.
type Recipient struct {
	Channel Channel
	Address string
}

// Validate checks the channel and the address SHAPE. Existence is the sender's
// job; the domain makes no network calls.
func (r Recipient) Validate() error {
	if !r.Channel.Valid() {
		return errs.InvalidInputErr("unknown channel").
			WithErr(ErrUnknownChannel).
			WithStr(fmt.Sprintf("got %q", r.Channel))
	}
	return r.Channel.ValidateTarget(r.Address)
}

// String is for logs and error detail only -- an address may be personal data.
func (r Recipient) String() string {
	return fmt.Sprintf("%s:%s", r.Channel, r.Address)
}
