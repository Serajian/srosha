package fcm

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// The codes this sender reads. FCM names them and documents what each means for
// a retry, so they lead and the http status is the fallback.
const (
	// errUnregistered: the token is dead. The app was uninstalled, or the
	// device rotated it. The clearest NOT_REACHABLE in the service so far --
	// there is a recipient, and they are gone.
	errUnregistered = "UNREGISTERED"

	// errSenderIDMismatch: a real token, issued by another project. Ours cannot
	// send to it and never will.
	errSenderIDMismatch = "SENDER_ID_MISMATCH"

	// errQuotaExceeded: too much, too fast, and temporary.
	errQuotaExceeded = "QUOTA_EXCEEDED"

	// errUnavailable and errInternal: Google's, and Google's to fix.
	errUnavailable = "UNAVAILABLE"
	errInternal    = "INTERNAL"

	// errThirdPartyAuth: FCM could not authenticate to APNs on our behalf --
	// an iOS certificate in the Firebase project, not the recipient.
	errThirdPartyAuth = "THIRD_PARTY_AUTH_ERROR"
)

// classify turns a refusal into the one question the core asks: is another
// attempt worth making?
func classify(resp *http.Response, body apiResponse) error {
	status := resp.StatusCode
	detail := body.Error.Message
	if detail == "" {
		detail = fmt.Sprintf("fcm answered %d", status)
	}

	code := body.Error.fcmCode()
	if code == "" {
		code = body.Error.Status
	}
	if code != "" {
		detail = code + ": " + detail
	}

	switch {
	case code == errUnregistered, code == errSenderIDMismatch:
		// The device, not the message. A source can act on this: stop sending
		// to that token.
		return &shared.SendError{Kind: shared.SendUnreachable, Detail: detail}

	case code == errQuotaExceeded, status == http.StatusTooManyRequests:
		return &shared.SendError{
			Kind:       shared.SendTransient,
			RetryAfter: retryAfter(resp),
			Detail:     detail,
		}

	case code == errUnavailable, code == errInternal:
		return &shared.SendError{
			Kind:       shared.SendTransient,
			RetryAfter: retryAfter(resp),
			Detail:     detail,
		}

	case code == errThirdPartyAuth:
		// Our Firebase project's problem, not this message's and not this
		// device's. Final, and a configuration answer.
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	case status >= 500:
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}

	case status >= 400:
		return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}

	default:
		return &shared.SendError{Kind: shared.SendTransient, Detail: detail}
	}
}

// retryAfter reads the header FCM sends with a quota refusal. Seconds or an
// http date, and either may be absent.
func retryAfter(resp *http.Response) time.Duration {
	raw := resp.Header.Get("Retry-After")
	if raw == "" {
		return defaultRetryAfter
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return defaultRetryAfter
}

// unreachable is every failure before an answer: dns, dial, tls, a timeout, a
// token that could not be minted. None of them say anything about the message.
//
// The url is dropped rather than quoted. It carries no secret -- the token
// travels in a header -- but net/http quotes the whole request url in a dial
// error, and there is no reason to carry the project id into a log line.
func unreachable(what string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &shared.SendError{
		Kind:   shared.SendTransient,
		Detail: fmt.Sprintf("fcm %s: %v", what, err),
		Err:    err,
	}
}

// refused is for what this sender decides on its own, before asking.
func refused(detail string) error {
	return &shared.SendError{Kind: shared.SendPermanent, Detail: detail}
}
