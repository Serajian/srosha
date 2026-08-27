package srosha_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/sdk/go/srosha"
)

const secret = "whsec_not-a-real-secret"

// signed builds what srosha would send. It is written out longhand rather than
// reusing the verifier's own arithmetic, so a change to either side has to be
// made twice on purpose.
func signed(t *testing.T, at time.Time, body string) http.Header {
	t.Helper()

	timestamp := strconv.FormatInt(at.Unix(), 10)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write([]byte(body))

	h := http.Header{}
	h.Set(srosha.SignatureHeader, "v1="+hex.EncodeToString(mac.Sum(nil)))
	h.Set(srosha.TimestampHeader, timestamp)
	return h
}

const callbackBody = `{
  "batch_id": "01K0BATCH0000000000000000B",
  "notification_id": "01K0NOTIF0000000000000000N",
  "sent_at": "2026-08-28T10:00:00Z",
  "results": [
    {
      "delivery_id": "01K0DELIV0000000000000000D",
      "channel": "telegram",
      "address": "-1001234",
      "status": "SENT",
      "settled_at": "2026-08-28T09:59:58Z",
      "provider_message_id": "4242"
    },
    {
      "delivery_id": "01K0DELIV0000000000000000E",
      "channel": "apns",
      "address": "a1b2c3",
      "status": "FAILED",
      "settled_at": "2026-08-28T09:59:59Z",
      "reason": "NOT_REACHABLE"
    }
  ]
}`

func verifier(t *testing.T, opts ...srosha.VerifierOption) *srosha.Verifier {
	t.Helper()

	v, err := srosha.NewVerifier(secret, opts...)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestAGenuineCallbackIsAccepted(t *testing.T) {
	now := time.Now()
	v := verifier(t)

	cb, err := v.Verify(signed(t, now, callbackBody), []byte(callbackBody))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if cb.ID != "01K0BATCH0000000000000000B" ||
		cb.NotificationID != "01K0NOTIF0000000000000000N" {
		t.Errorf("callback = %+v", cb)
	}
	if len(cb.Deliveries) != 2 {
		t.Fatalf("got %d deliveries, want 2", len(cb.Deliveries))
	}

	sent := cb.Deliveries[0]
	if sent.Channel != srosha.ChannelTelegram || sent.Status != srosha.StatusSent {
		t.Errorf("first = %+v", sent)
	}
	if sent.ProviderMessageID != "4242" || sent.Reason != srosha.FailureNone {
		t.Errorf("first = %+v", sent)
	}

	failed := cb.Deliveries[1]
	if failed.Channel != srosha.ChannelAPNs || failed.Status != srosha.StatusFailed {
		t.Errorf("second = %+v", failed)
	}
	if failed.Reason != srosha.FailureNotReachable {
		t.Errorf("second reason = %q, want not_reachable", failed.Reason)
	}
}

// The callback spells these in capitals -- "SENT", "NOT_REACHABLE" -- while
// everything else in this package is lower case. Left as they arrive, a caller
// comparing against StatusSent would silently never match.
func TestTheCallbacksCapitalsBecomeThisPackagesWords(t *testing.T) {
	now := time.Now()
	v := verifier(t)

	cb, err := v.Verify(signed(t, now, callbackBody), []byte(callbackBody))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	for _, d := range cb.Deliveries {
		if strings.ToUpper(string(d.Status)) == string(d.Status) && d.Status != "" {
			t.Errorf("status %q came through in capitals", d.Status)
		}
		if d.Reason != "" && strings.ToUpper(string(d.Reason)) == string(d.Reason) {
			t.Errorf("reason %q came through in capitals", d.Reason)
		}
	}
}

func TestABodyChangedInFlight(t *testing.T) {
	v := verifier(t)
	headers := signed(t, time.Now(), callbackBody)

	tampered := strings.Replace(callbackBody, `"status": "FAILED"`, `"status": "SENT"`, 1)

	_, err := v.Verify(headers, []byte(tampered))
	if !errors.Is(err, srosha.ErrSignatureInvalid) {
		t.Errorf("Verify = %v, want ErrSignatureInvalid", err)
	}
}

func TestSomebodyElsesSecret(t *testing.T) {
	other, err := srosha.NewVerifier("whsec_a-different-secret")
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	_, err = other.Verify(signed(t, time.Now(), callbackBody), []byte(callbackBody))
	if !errors.Is(err, srosha.ErrSignatureInvalid) {
		t.Errorf("Verify = %v, want ErrSignatureInvalid", err)
	}
}

// A replay: authentic bytes, posted again later. The window is exactly how long
// that keeps working.
func TestACallbackCapturedAndPostedLater(t *testing.T) {
	v := verifier(t, srosha.WithTolerance(5*time.Minute))

	old := signed(t, time.Now().Add(-6*time.Minute), callbackBody)

	_, err := v.Verify(old, []byte(callbackBody))
	if !errors.Is(err, srosha.ErrCallbackTooOld) {
		t.Errorf("Verify = %v, want ErrCallbackTooOld", err)
	}
}

// A clock can be wrong in either direction, and a timestamp ahead of us is as
// good a sign of trouble as one behind.
func TestATimestampFromTheFuture(t *testing.T) {
	v := verifier(t, srosha.WithTolerance(time.Minute))

	ahead := signed(t, time.Now().Add(10*time.Minute), callbackBody)

	_, err := v.Verify(ahead, []byte(callbackBody))
	if !errors.Is(err, srosha.ErrCallbackTooOld) {
		t.Errorf("Verify = %v, want ErrCallbackTooOld", err)
	}
}

func TestInsideTheWindowIsFine(t *testing.T) {
	v := verifier(t, srosha.WithTolerance(5*time.Minute))

	for _, drift := range []time.Duration{-4 * time.Minute, -time.Second, 4 * time.Minute} {
		headers := signed(t, time.Now().Add(drift), callbackBody)
		if _, err := v.Verify(headers, []byte(callbackBody)); err != nil {
			t.Errorf("drift %v: %v", drift, err)
		}
	}
}

func TestWhatIsNotASignedCallbackAtAll(t *testing.T) {
	v := verifier(t)
	now := strconv.FormatInt(time.Now().Unix(), 10)

	cases := map[string]http.Header{
		"nothing at all": {},
		"no signature":   {srosha.TimestampHeader: []string{now}},
		"no timestamp":   {srosha.SignatureHeader: []string{"v1=abcd"}},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(headers, []byte(callbackBody)); !errors.Is(
				err,
				srosha.ErrSignatureMissing,
			) {
				t.Errorf("Verify = %v, want ErrSignatureMissing", err)
			}
		})
	}
}

func TestASignatureThisBuildCannotCheck(t *testing.T) {
	v := verifier(t)
	now := strconv.FormatInt(time.Now().Unix(), 10)

	cases := map[string]string{
		"a version we do not know": "v2=abcd",
		"no version at all":        "abcd",
		"not hex":                  "v1=zzzz",
	}
	for name, signature := range cases {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			h.Set(srosha.SignatureHeader, signature)
			h.Set(srosha.TimestampHeader, now)

			if _, err := v.Verify(h, []byte(callbackBody)); !errors.Is(
				err,
				srosha.ErrSignatureInvalid,
			) {
				t.Errorf("Verify = %v, want ErrSignatureInvalid", err)
			}
		})
	}
}

// The signature covers the exact bytes srosha sent, so anything that re-encodes
// the body breaks it -- which is the whole reason the doc says to read it raw.
func TestReEncodingTheBodyBreaksIt(t *testing.T) {
	v := verifier(t)
	headers := signed(t, time.Now(), callbackBody)

	// The same json, formatted differently. Every field and value is identical.
	compact := strings.Join(strings.Fields(callbackBody), "")

	if _, err := v.Verify(headers, []byte(compact)); !errors.Is(err, srosha.ErrSignatureInvalid) {
		t.Errorf("Verify = %v, want it refused", err)
	}
}

// Nothing is parsed before it is authenticated.
func TestAForgedCallbackIsNeverParsed(t *testing.T) {
	v := verifier(t)

	h := http.Header{}
	h.Set(srosha.SignatureHeader, "v1="+strings.Repeat("ab", 32))
	h.Set(srosha.TimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))

	// Not json at all. A verifier that parsed first would say so; this one
	// never gets that far.
	_, err := v.Verify(h, []byte("this is not json"))
	if !errors.Is(err, srosha.ErrSignatureInvalid) {
		t.Errorf("Verify = %v, want the signature refused first", err)
	}
}

func TestAVerifierNeedsASecretAndAWindow(t *testing.T) {
	if _, err := srosha.NewVerifier("  "); err == nil {
		t.Error("NewVerifier with no secret succeeded")
	}
	if _, err := srosha.NewVerifier(secret, srosha.WithTolerance(0)); err == nil {
		t.Error("NewVerifier with no tolerance succeeded")
	}
}
