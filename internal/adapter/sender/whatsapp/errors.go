package whatsapp

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Serajian/srosha/internal/core/shared"
)

// The error codes this sender reads.
//
// Meta has hundreds and documents them loosely, so the http status carries most
// of the decision and these are the few where the status alone would say the
// wrong thing. They are the part of this file most likely to need changing --
// which is why the status is the rule and these are the exceptions, rather than
// the other way round.
const (
	// codeReengagement: the message is outside the window the recipient opened.
	// A 4xx, but not the message's fault -- the same text sent after they write
	// again goes through.
	codeReengagement = 131047

	// codeUndeliverable: the number is not on WhatsApp, or cannot receive.
	codeUndeliverable = 131026

	// codeRateLimit and codePairRateLimit: too much, too fast. Theirs, and
	// temporary.
	codeRateLimit     = 130429
	codePairRateLimit = 131056
)

// classify turns Meta's refusal into the one question the core asks: is another
// attempt worth making?
//
// The token is stripped from what it says, and that is not belt and braces:
// Meta quotes the credential back in its own message -- "Malformed access token
// EAAG..." -- and this detail is written to the delivery row. The token travels
// in a header rather than the url, so it is the ANSWER that leaks it here and
// not the request.
func classify(status int, body apiResponse, token string) error {
	detail := fmt.Sprintf("whatsapp answered %d", status)
	code := 0
	if body.Error != nil {
		code = body.Error.Code
		if body.Error.Message != "" {
			detail = redact(body.Error.Message, token)
		}
	}

	switch {
	case code == codeReengagement, code == codeUndeliverable:
		// The recipient, not the message. Nothing the source wrote differently
		// would have helped: somebody has to write to them first, or the number
		// is not on WhatsApp at all.
		return &shared.SendError{Kind: shared.SendUnreachable, Detail: detail}

	case code == codeRateLimit, code == codePairRateLimit, status == 429:
		return &shared.SendError{
			Kind:       shared.SendTransient,
			RetryAfter: defaultRetryAfter,
			Detail:     detail,
		}

	case status >= 500:
		// Theirs, and theirs to fix. Nothing about this message changes it.
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}

	case status >= 400:
		// A template that is not approved, a body they will never accept, a
		// token that is not one. Repeating gets the same answer until somebody
		// changes something.
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	default:
		// A 2xx with an error in it, or none of the above. Not documented, so
		// it is not claimed to be final.
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}
	}
}

// unreachable is every failure before an answer: dns, dial, tls, a timeout, a
// context that expired. None of them say anything about the message.
//
// The url is not a secret here, unlike the bot channels: the token travels in a
// header rather than in the path. The phone number id is in it, though, and that
// is somebody's account, so the url is dropped rather than quoted.
func unreachable(what string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &shared.SendError{
		Kind:   shared.SendTransient,
		Detail: fmt.Sprintf("whatsapp %s: %v", what, err),
		Err:    err,
	}
}

// redact keeps the token out of whatever the provider said back.
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
