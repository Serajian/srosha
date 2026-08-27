package matrix

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// The error codes this sender reads.
//
// Matrix names its errors rather than numbering them, which makes this the one
// channel where the code is stable enough to lead on. The http status still
// carries whatever is not listed here.
const (
	// errForbidden: not in the room, or not allowed to speak in it. The room
	// refusing us, which is this protocol's version of a recipient refusing us.
	errForbidden = "M_FORBIDDEN"

	// errNotFound: no such room. Same conclusion, different cause.
	errNotFound = "M_NOT_FOUND"

	// errUnknownToken and errMissingToken: our credential, not the recipient.
	errUnknownToken = "M_UNKNOWN_TOKEN"
	errMissingToken = "M_MISSING_TOKEN"

	// errLimitExceeded: too much, too fast. Theirs, and temporary.
	errLimitExceeded = "M_LIMIT_EXCEEDED"
)

// classify turns a homeserver's refusal into the one question the core asks: is
// another attempt worth making?
func classify(status int, body apiResponse) error {
	detail := body.Error
	if detail == "" {
		detail = fmt.Sprintf("matrix answered %d", status)
	}
	if body.ErrCode != "" {
		detail = body.ErrCode + ": " + detail
	}

	switch {
	case body.ErrCode == errForbidden, body.ErrCode == errNotFound:
		// The room, not the message. Nothing the source wrote differently would
		// have helped: somebody has to invite us, or the room is gone.
		return &shared.SendError{Kind: shared.SendUnreachable, Detail: detail}

	case body.ErrCode == errUnknownToken, body.ErrCode == errMissingToken:
		// Our credential. Final for this message, and a configuration answer
		// rather than anything about the recipient.
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	case body.ErrCode == errLimitExceeded, status == 429:
		return &shared.SendError{
			Kind:       shared.SendTransient,
			RetryAfter: retryAfter(body),
			Detail:     detail,
		}

	case status >= 500:
		// Theirs, and theirs to fix. Nothing about this message changes it.
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}

	case status >= 400:
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	default:
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}
	}
}

func retryAfter(body apiResponse) time.Duration {
	if body.RetryAfterMs > 0 {
		return time.Duration(body.RetryAfterMs) * time.Millisecond
	}
	return defaultRetryAfter
}

// unreachable is every failure before an answer: dns, dial, tls, a timeout, a
// context that expired. None of them say anything about the message.
//
// The url is dropped rather than quoted. It carries no token -- that travels in
// a header -- but it does carry the homeserver and the room, and a room id is
// somebody's address.
func unreachable(what string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &shared.SendError{
		Kind:   shared.SendTransient,
		Detail: fmt.Sprintf("matrix %s: %v", what, err),
		Err:    err,
	}
}

// refused is for what this sender decides on its own, before asking.
func refused(detail string) error {
	return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}
}
