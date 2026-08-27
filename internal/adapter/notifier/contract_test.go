package notifier_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/notifier"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/sdk/go/srosha"
)

// send posts one batch through the real notifier at a chosen moment, and hands
// back exactly what arrived on the other side.
func send(t *testing.T, secret string, at time.Time, b webhook.Batch) (http.Header, []byte) {
	t.Helper()

	var (
		headers http.Header
		body    []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	n, err := notifier.New(
		server.Client(), secrets{sourceID: secret},
		func() time.Time { return at },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("notifier.New: %v", err)
	}

	w := webhook.Restore(webhook.Snapshot{
		ID: shared.ID("01K0HOOK00000000000000000W"), SourceID: sourceID,
		CallbackURL: server.URL, IsActive: true, CreatedAt: at, UpdatedAt: at,
	})
	if err := n.Notify(context.Background(), w, b); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	return headers, body
}

// The other half of the seam.
//
// This service signs a callback and the SDK checks it, and until now nothing
// ran both. A separator, a header name, a capital letter in a status -- any of
// them could drift on one side and be found by a customer, on a real callback,
// whose only symptom is a signature that will not verify.
//
// It is possible for the same reason the credential test is: the SDK cannot
// import this service, but this service can import the SDK.
func TestTheSDKVerifiesWhatThisServiceSigns(t *testing.T) {
	const secret = "whsec_shared-between-both-halves"

	now := time.Now()
	headers, body := send(t, secret, now, webhook.Batch{
		ID:             shared.ID("01K0BATCH0000000000000000B"),
		NotificationID: shared.ID("01K0NOTIF0000000000000000N"),
		SentAt:         now,
		Results: []webhook.Result{
			{
				DeliveryID:        shared.ID("01K0DELIV0000000000000000D"),
				Channel:           shared.ChannelTelegram.String(),
				Address:           "-1001234",
				Status:            "SENT",
				SettledAt:         now,
				ProviderMessageID: "4242",
			},
			{
				DeliveryID: shared.ID("01K0DELIV0000000000000000E"),
				Channel:    shared.ChannelAPNs.String(),
				Address:    "a1b2c3d4",
				Status:     "FAILED",
				SettledAt:  now,
				Reason:     "NOT_REACHABLE",
			},
		},
	})

	// --- and now the customer's side, sharing no code with the above ---

	v, err := srosha.NewVerifier(secret)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	cb, err := v.Verify(headers, body)
	if err != nil {
		t.Fatalf("the SDK could not verify what we signed: %v", err)
	}

	if cb.ID != "01K0BATCH0000000000000000B" {
		t.Errorf("batch id = %q", cb.ID)
	}
	if cb.NotificationID != "01K0NOTIF0000000000000000N" {
		t.Errorf("notification id = %q", cb.NotificationID)
	}
	if len(cb.Deliveries) != 2 {
		t.Fatalf("got %d deliveries, want 2", len(cb.Deliveries))
	}

	// The values the SDK hands a customer must be the words it documents, not
	// the capitals this side writes.
	sent := cb.Deliveries[0]
	if sent.Channel != srosha.ChannelTelegram || sent.Status != srosha.StatusSent {
		t.Errorf("sent = %+v", sent)
	}
	if sent.ProviderMessageID != "4242" {
		t.Errorf("provider id = %q", sent.ProviderMessageID)
	}

	failed := cb.Deliveries[1]
	if failed.Channel != srosha.ChannelAPNs || failed.Status != srosha.StatusFailed {
		t.Errorf("failed = %+v", failed)
	}
	if failed.Reason != srosha.FailureNotReachable {
		t.Errorf("reason = %q, want %q", failed.Reason, srosha.FailureNotReachable)
	}
}

// The header names are this service's contract with every receiver, so the SDK
// carrying its own copy of them is only safe while they are the same words.
func TestTheHeaderNamesAreTheSameOnBothSides(t *testing.T) {
	now := time.Now()
	headers, _ := send(t, "whsec_x", now, webhook.Batch{
		ID: shared.ID("01K0BATCH0000000000000000B"), SentAt: now,
	})

	if headers.Get(srosha.SignatureHeader) == "" {
		t.Errorf("nothing arrived under %q", srosha.SignatureHeader)
	}
	if headers.Get(srosha.TimestampHeader) == "" {
		t.Errorf("nothing arrived under %q", srosha.TimestampHeader)
	}
}

// A receiver whose clock is far from ours refuses a callback that is otherwise
// perfectly genuine. That is the tolerance doing its job, and worth seeing
// against a real signature rather than a hand-built one.
func TestAGenuineCallbackStillGoesStale(t *testing.T) {
	const secret = "whsec_x"

	signedAgo := time.Now().Add(-10 * time.Minute)
	headers, body := send(t, secret, signedAgo, webhook.Batch{
		ID: shared.ID("01K0BATCH0000000000000000B"), SentAt: signedAgo,
	})

	v, err := srosha.NewVerifier(secret, srosha.WithTolerance(5*time.Minute))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if _, err := v.Verify(headers, body); !errors.Is(err, srosha.ErrCallbackTooOld) {
		t.Errorf("Verify = %v, want ErrCallbackTooOld", err)
	}
}
