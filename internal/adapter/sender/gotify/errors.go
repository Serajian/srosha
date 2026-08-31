package gotify

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/Serajian/srosha/internal/core/shared"
)

// classify turns Gotify's answer into the one question the core asks: is
// another attempt worth making?
//
// Gotify does not document a machine-readable vocabulary of error codes the
// way Matrix does, so the http status carries the decision here and apiError
// is read only for Detail, on a best-effort basis -- see the assumption on
// apiError.
//
// There is deliberately no case mapped to SendUnreachable. That kind means
// the provider refused the RECIPIENT rather than the message, and Gotify's
// documented API has no notion of one: it stores a message for an
// application's subscribers and reports nothing back about any of them
// individually, unlike a blocked chat or an expired device token elsewhere.
// If the owner's server can in fact say that, this is the file to extend.
func classify(status int, body apiError) error {
	detail := body.ErrorDescription
	if detail == "" {
		detail = fmt.Sprintf("gotify answered %d", status)
	}
	if body.Error != "" {
		detail = body.Error + ": " + detail
	}

	switch {
	case status == 401, status == 403:
		// The credential, not the recipient: an application token Gotify does
		// not recognize. Final for this message, and a configuration answer
		// rather than anything about who would have received it.
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	case status == 429:
		return &shared.SendError{
			Kind: shared.SendTransient, RetryAfter: defaultRetryAfter, Detail: detail,
		}

	case status >= 500:
		// Theirs, and theirs to fix. Nothing about this message changes it.
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}

	case status >= 400:
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	default:
		// Undocumented, so it is not claimed to be final.
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}
	}
}

// unreachable is every failure before an answer: dns, dial, tls, a timeout, a
// context that expired. None of them say anything about the message.
//
// The url is dropped rather than quoted. It carries the application token and
// the application id in its query string -- see (*Sender).endpoint -- and
// neither belongs in a log a person reads.
func unreachable(what string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &shared.SendError{
		Kind:   shared.SendTransient,
		Detail: fmt.Sprintf("gotify %s: %v", what, err),
		Err:    err,
	}
}

// refused is for what this sender decides on its own, before asking.
func refused(detail string) error {
	return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}
}
