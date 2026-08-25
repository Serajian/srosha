package bale

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// classify turns the API's answer into the one question the core asks: is
// another attempt worth making?
//
// Mapping a provider's vocabulary into that is the whole reason this package
// exists. Getting it wrong is expensive in both directions: a permanent failure
// treated as transient occupies the queue until the delivery limit runs out,
// and a transient one treated as permanent throws away a message that would
// have gone out a minute later.
func classify(status int, body apiResponse) error {
	detail := body.Description
	if detail == "" {
		detail = fmt.Sprintf("bale answered %d", status)
	}

	switch {
	case status == 429:
		return &shared.SendError{
			Kind:       shared.SendTransient,
			RetryAfter: retryAfter(body),
			Detail:     detail,
		}

	case status >= 500:
		// Theirs, and theirs to fix. Nothing about this message changes it.
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}

	case status == 403:
		// The recipient refused us: the bot was blocked, or it has no access to
		// that chat. Final like any other 4xx, and separate because the source
		// can act on it -- nothing they write differently would have helped.
		return &shared.SendError{Kind: shared.SendUnreachable, Detail: detail}

	case status >= 400:
		// A chat that does not exist, a bot that was blocked, a token that is
		// not one. Different causes, one conclusion: repeating the same request
		// gets the same answer until a person changes something.
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	default:
		// ok:false with a 2xx. Not documented, so it is not claimed to be final.
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}
	}
}

// retryAfter uses the API's own wait when it states one, and a wait of our own
// when it does not. Sending again immediately after a 429 is how a limit that
// would have cleared becomes one that keeps being earned.
func retryAfter(body apiResponse) time.Duration {
	if body.Parameters != nil && body.Parameters.RetryAfter > 0 {
		return time.Duration(body.Parameters.RetryAfter) * time.Second
	}
	return defaultRetryAfter
}

// unreachable is every failure before an answer: dns, dial, tls, a timeout, a
// context that expired. None of them say anything about the message.
//
// The token is stripped, and that is not belt and braces. The credential is in
// the PATH, so net/http quotes the whole url in its own errors and every dial
// failure would otherwise carry a working token into a log.
func unreachable(what, token string, err error) error {
	// The url, and the token in it, stops here.
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}

	return &shared.SendError{
		Kind:   shared.SendTransient,
		Detail: redact(fmt.Sprintf("bale %s: %v", what, err), token),
		Err:    err,
	}
}

// redact is the backstop for whatever unwrapping does not reach.
func redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "[REDACTED]")
}

// refused is for what this sender decides on its own, before asking.
func refused(detail string) error {
	return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}
}
