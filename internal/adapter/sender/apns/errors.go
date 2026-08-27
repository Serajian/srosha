package apns

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Serajian/srosha/internal/core/shared"
)

// The reasons this sender reads. APNs answers with one word and nothing else,
// so the word decides and the status is the fallback.
const (
	// reasonBadDeviceToken: not a token for this environment or this topic.
	// About the recipient, and the source can act on it.
	reasonBadDeviceToken = "BadDeviceToken"

	// reasonUnregistered: the app was removed from that device. The clearest
	// NOT_REACHABLE there is.
	reasonUnregistered = "Unregistered"

	// reasonNotForTopic: a real token, for a different app.
	reasonNotForTopic = "DeviceTokenNotForTopic"

	// reasonExpiredToken: our provider token aged out. Worth another attempt --
	// but only with a new one, which is why it is handled and not just
	// classified.
	reasonExpiredToken = "ExpiredProviderToken"

	// reasonTooManyTokenUpdates: we signed provider tokens too often. Also
	// temporary, and the opposite treatment: signing another makes it worse.
	reasonTooManyTokenUpdates = "TooManyProviderTokenUpdates"

	// reasonTooManyRequests: too much for one device.
	reasonTooManyRequests = "TooManyRequests"

	// reasonInternal and reasonUnavailable: Apple's, and Apple's to fix.
	reasonInternal    = "InternalServerError"
	reasonUnavailable = "ServiceUnavailable"
)

// classify turns a refusal into the one question the core asks: is another
// attempt worth making?
func classify(status int, body apiResponse) error {
	detail := body.Reason
	if detail == "" {
		detail = fmt.Sprintf("apns answered %d", status)
	}
	if body.Reason == reasonUnregistered && body.Timestamp > 0 {
		detail = fmt.Sprintf("%s (since %d)", detail, body.Timestamp)
	}

	switch {
	case body.Reason == reasonBadDeviceToken,
		body.Reason == reasonUnregistered,
		body.Reason == reasonNotForTopic:
		// The device, not the message. Nothing the source wrote differently
		// would have helped, and they can act on it: stop sending there.
		return &shared.SendError{Kind: shared.SendUnreachable, Detail: detail}

	case body.Reason == reasonExpiredToken,
		body.Reason == reasonTooManyTokenUpdates,
		body.Reason == reasonTooManyRequests,
		body.Reason == reasonInternal,
		body.Reason == reasonUnavailable,
		status == http.StatusTooManyRequests:
		return &shared.SendError{
			Kind:       shared.SendTransient,
			RetryAfter: defaultRetryAfter,
			Detail:     detail,
		}

	case status >= 500:
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}

	case status >= 400:
		// Everything else APNs says is about our configuration or our payload:
		// a bad topic, a key it does not know, a message too large. Final.
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	default:
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}
	}
}

// unreachable is every failure before an answer: dns, dial, tls, a timeout.
// None of them say anything about the message.
//
// The url is dropped rather than quoted. It carries no secret -- the provider
// token travels in a header -- but the path is the device token, and that is
// somebody's address.
func unreachable(what string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &shared.SendError{
		Kind:   shared.SendTransient,
		Detail: fmt.Sprintf("apns %s: %v", what, err),
		Err:    err,
	}
}

// refused is for what this sender decides on its own, before asking.
func refused(detail string) error {
	return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}
}
