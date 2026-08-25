package email

import (
	"errors"

	"github.com/Serajian/srosha/internal/core/shared"
)

// classify turns an SMTP failure into the one question the core asks: is another
// attempt worth making?
//
// SMTP answers it itself, in the first digit of every reply, and it is one of
// the few protocols that does. Reading that digit is infra's; deciding what it
// means for a retry is this package's, because "worth another attempt" is a
// statement about how srosha sends and not about how mail works.
func classify(err error) error {
	code := replyCode(err)

	switch {
	case code >= 500:
		// No such mailbox, message refused, relay denied, credentials wrong.
		// Different causes, one conclusion: repeating gets the same answer
		// until a person changes something.
		return &shared.SendError{Permanent: true, Detail: err.Error(), Err: err}

	case code >= 400:
		// Busy, greylisted, out of space for now. Theirs, and temporary.
		return &shared.SendError{Permanent: false, Detail: err.Error(), Err: err}

	default:
		// No code at all: dns, dial, tls, a connection dropped mid-session.
		// None of them say anything about the message. Unclassified counts as
		// transient too, as shared.IsPermanentSend says -- an unknown failure
		// is more often a blip than a dead end, and the delivery limit stops
		// the loop either way.
		return &shared.SendError{Permanent: false, Detail: err.Error(), Err: err}
	}
}

// replyCode reads the code out of any error that carries one.
//
// An interface rather than a type, so this package never has to know which one
// infra returns -- and so a second mail transport could answer the same question
// without this changing.
func replyCode(err error) int {
	var coded interface{ ReplyCode() int }
	if errors.As(err, &coded) {
		return coded.ReplyCode()
	}
	return 0
}
