package srosha

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// What verifying a callback can go wrong with.
//
// Three, because a receiver acts differently on each: a missing signature is
// somebody who is not srosha, an invalid one is somebody pretending to be, and
// a stale one is either a replay or two clocks that have drifted apart.
var (
	// ErrSignatureMissing: the request carried no signature, or no timestamp.
	// srosha always sends both.
	ErrSignatureMissing = errors.New("srosha: callback is not signed")

	// ErrSignatureInvalid: the signature does not match. Either the body was
	// changed in flight, or it was not signed with your secret.
	ErrSignatureInvalid = errors.New("srosha: callback signature does not match")

	// ErrCallbackTooOld: authentic, but signed too long ago to accept.
	//
	// Almost always a replay -- somebody posting a callback they captured. The
	// other cause is a clock: if this fires on live traffic, check that this
	// machine's time is right before widening the tolerance.
	ErrCallbackTooOld = errors.New("srosha: callback is outside the accepted time window")
)

// Callback is one delivery-status push: what settled since the last one.
type Callback struct {
	// ID names this push, and is for tracing only. To tell duplicates apart
	// use a delivery's own id: a delivery settles once and never changes, while
	// a callback is whatever had finished at the moment it was built.
	ID string

	NotificationID string

	// SentAt is when srosha built it, not when a delivery settled.
	SentAt time.Time

	Deliveries []CallbackDelivery
}

// CallbackDelivery is one recipient's settled outcome.
//
// A type of its own rather than Delivery, because a callback carries less: it
// says nothing about which identity sent, and a field that is always empty is
// a field that lies.
type CallbackDelivery struct {
	ID      string
	Channel Channel
	Address string

	// Status is StatusSent or StatusFailed. A callback is only sent for a
	// delivery that settled, so it is never StatusPending.
	Status Status

	// Reason is set only when Status is StatusFailed.
	Reason FailureReason

	// ProviderMessageID is set only when it was sent.
	ProviderMessageID string

	SettledAt time.Time
}

// Verifier checks that a callback really came from srosha.
//
// Build one at startup with the signing secret srosha's operator gave you and
// keep it: it holds nothing that changes.
//
// It deliberately is not an http.Handler. Wiring it into a route is three lines
// and belongs to whatever framework you use; a handler here would tie this
// package to one.
type Verifier struct {
	secret    string
	tolerance time.Duration
	now       func() time.Time
}

// VerifierOption changes how strict verification is.
type VerifierOption func(*Verifier)

// WithTolerance changes how far a callback's timestamp may be from now.
//
// Widen it only for clocks you cannot fix, and know what it costs: the window
// is exactly how long a captured callback stays replayable.
func WithTolerance(d time.Duration) VerifierOption {
	return func(v *Verifier) { v.tolerance = d }
}

// NewVerifier takes the signing secret.
//
// It is per source, and Webhooks.Register hands it over on the call that
// creates the callback -- the only time anything does. srosha keeps it
// encrypted and no rpc reads it back; RotateSecret issues a new one for a
// source that lost theirs.
func NewVerifier(secret string, opts ...VerifierOption) (*Verifier, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("srosha: no signing secret")
	}

	v := &Verifier{secret: secret, tolerance: defaultTolerance, now: time.Now}
	for _, apply := range opts {
		apply(v)
	}
	if v.tolerance <= 0 {
		return nil, errors.New("srosha: tolerance must be above zero")
	}
	return v, nil
}

// Verify checks the signature and returns what srosha is telling you.
//
//	body, err := io.ReadAll(r.Body)
//	if err != nil {
//	    http.Error(w, "", http.StatusBadRequest)
//	    return
//	}
//	cb, err := verifier.Verify(r.Header, body)
//	if err != nil {
//	    http.Error(w, "", http.StatusUnauthorized)
//	    return
//	}
//
// Read the whole body first and hand it over unchanged: the signature covers
// the exact bytes srosha sent, so a body that has been re-encoded, pretty
// printed or read through a json decoder will not verify.
//
// It takes an http.Header rather than two strings so the header names cannot be
// got wrong, and because every framework can hand you one. That is the standard
// library, not a web framework.
//
// The signature is checked before the body is parsed. Nothing here interprets
// bytes it has not authenticated.
func (v *Verifier) Verify(headers http.Header, body []byte) (Callback, error) {
	signature := headers.Get(SignatureHeader)
	timestamp := headers.Get(TimestampHeader)
	if signature == "" || timestamp == "" {
		return Callback{}, ErrSignatureMissing
	}

	if err := v.check(signature, timestamp, body); err != nil {
		return Callback{}, err
	}

	// Only now is the timestamp known to be srosha's rather than the caller's,
	// which is why staleness is checked after the signature and not before.
	if err := v.fresh(timestamp); err != nil {
		return Callback{}, err
	}

	return decodeCallback(body)
}

// check compares the signature in constant time. A comparison that returns
// early tells whoever is guessing how much of their guess was right.
func (v *Verifier) check(signature, timestamp string, body []byte) error {
	version, sum, ok := strings.Cut(signature, "=")
	if !ok || version != signatureVersion {
		// A version this build does not know is not a signature it can check,
		// and treating it as invalid is the only safe answer.
		return fmt.Errorf("%w: unknown signature version", ErrSignatureInvalid)
	}

	given, err := hex.DecodeString(sum)
	if err != nil {
		return fmt.Errorf("%w: signature is not hex", ErrSignatureInvalid)
	}

	mac := hmac.New(sha256.New, []byte(v.secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(signedSeparator))
	mac.Write(body)

	if !hmac.Equal(given, mac.Sum(nil)) {
		return ErrSignatureInvalid
	}
	return nil
}

// fresh refuses a callback signed too long ago, or too far in the future.
//
// Both directions, because a clock can be wrong either way and a timestamp
// ahead of us is as good a sign of trouble as one behind.
func (v *Verifier) fresh(timestamp string) error {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: timestamp is not a unix time", ErrSignatureInvalid)
	}

	drift := v.now().Sub(time.Unix(seconds, 0))
	if drift < 0 {
		drift = -drift
	}
	if drift > v.tolerance {
		return fmt.Errorf("%w: signed %s away from now", ErrCallbackTooOld, drift.Round(time.Second))
	}
	return nil
}

// wire is the callback exactly as it arrives. It is separate from Callback so
// that the field names on the wire and the names a caller reads can each change
// without the other.
type wire struct {
	ID             string    `json:"batch_id"`
	NotificationID string    `json:"notification_id"`
	SentAt         time.Time `json:"sent_at"`

	Results []struct {
		DeliveryID        string    `json:"delivery_id"`
		Channel           string    `json:"channel"`
		Address           string    `json:"address"`
		Status            string    `json:"status"`
		SettledAt         time.Time `json:"settled_at"`
		Reason            string    `json:"reason"`
		ProviderMessageID string    `json:"provider_message_id"`
	} `json:"results"`
}

func decodeCallback(body []byte) (Callback, error) {
	var w wire
	if err := json.Unmarshal(body, &w); err != nil {
		// Authentic but unreadable: srosha sent it, so this is a version of it
		// this build does not understand rather than an attack.
		return Callback{}, fmt.Errorf("srosha: callback body could not be read: %w", err)
	}

	out := Callback{
		ID:             w.ID,
		NotificationID: w.NotificationID,
		SentAt:         w.SentAt,
		Deliveries:     make([]CallbackDelivery, 0, len(w.Results)),
	}
	for _, r := range w.Results {
		out.Deliveries = append(out.Deliveries, CallbackDelivery{
			ID:      r.DeliveryID,
			Channel: Channel(strings.ToLower(r.Channel)),
			Address: r.Address,
			// Lowercased because the callback spells these in capitals --
			// "SENT", "NOT_REACHABLE" -- while everything else in this package
			// is lower case. Left as they arrive, a caller comparing against
			// StatusSent would silently never match.
			Status:            Status(strings.ToLower(r.Status)),
			Reason:            FailureReason(strings.ToLower(r.Reason)),
			ProviderMessageID: r.ProviderMessageID,
			SettledAt:         r.SettledAt,
		})
	}
	return out, nil
}
