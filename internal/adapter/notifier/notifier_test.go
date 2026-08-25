package notifier_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/notifier"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	sourceID = "01K0SRC0000000000000000000"
	secret   = "a-signing-secret"
)

var at = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

type secrets map[string]string

func (s secrets) SecretFor(id string) (string, bool) {
	v, ok := s[id]
	return v, ok
}

type call struct {
	body      []byte
	signature string
	timestamp string
	seen      bool
}

func callback(t *testing.T, status int) (*notifier.Notifier, *webhook.Webhook, *call) {
	t.Helper()
	return callbackWith(t, status, secrets{sourceID: secret})
}

func callbackWith(t *testing.T, status int, keys secrets) (*notifier.Notifier, *webhook.Webhook, *call) {
	t.Helper()

	got := &call{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.seen = true
		got.body, _ = io.ReadAll(r.Body)
		got.signature = r.Header.Get("X-Srosha-Signature")
		got.timestamp = r.Header.Get("X-Srosha-Timestamp")
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	n, err := notifier.New(
		server.Client(), keys,
		func() time.Time { return at },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := webhook.Restore(webhook.Snapshot{
		ID: shared.ID("01K0HOOK00000000000000000W"), SourceID: sourceID,
		CallbackURL: server.URL, IsActive: true,
		CreatedAt: at, UpdatedAt: at,
	})
	return n, w, got
}

func batch() webhook.Batch {
	return webhook.Batch{
		ID:             shared.ID("01K0BATCH0000000000000000B"),
		NotificationID: shared.ID("01K0NOTIF0000000000000000N"),
		SentAt:         at,
		Results: []webhook.Result{{
			DeliveryID: shared.ID("01K0DELIV0000000000000000D"),
			Channel:    "telegram",
			Address:    "-1001234",
			Status:     "SENT",
			SettledAt:  at,

			ProviderMessageID: "4242",
		}},
	}
}

func TestTheOutcomeReachesTheCallback(t *testing.T) {
	n, w, got := callback(t, http.StatusOK)

	if err := n.Notify(context.Background(), w, batch()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var sent webhook.Batch
	if err := json.Unmarshal(got.body, &sent); err != nil {
		t.Fatalf("the body is not the batch: %v", err)
	}
	if sent.NotificationID != batch().NotificationID || len(sent.Results) != 1 {
		t.Errorf("body = %s", got.body)
	}
	if sent.Results[0].ProviderMessageID != "4242" {
		t.Errorf("the provider id did not travel: %s", got.body)
	}
}

// What a receiver actually has to do, done here to prove it can be done.
func TestASourceCanVerifyWhatItGot(t *testing.T) {
	n, w, got := callback(t, http.StatusOK)

	if err := n.Notify(context.Background(), w, batch()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(got.timestamp))
	mac.Write([]byte("."))
	mac.Write(got.body)
	want := "v1=" + hex.EncodeToString(mac.Sum(nil))

	if got.signature != want {
		t.Errorf("signature = %q, want %q", got.signature, want)
	}
	if got.timestamp != strconv.FormatInt(at.Unix(), 10) {
		t.Errorf("timestamp = %q, want unix seconds", got.timestamp)
	}
}

// The reason the timestamp is signed and not merely sent: a signature over the
// body alone is valid for ever, so anyone who saw one callback can replay it.
func TestTheTimestampIsPartOfWhatIsSigned(t *testing.T) {
	n, w, got := callback(t, http.StatusOK)

	if err := n.Notify(context.Background(), w, batch()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// The same body, claimed to be from an hour later.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(at.Add(time.Hour).Unix(), 10)))
	mac.Write([]byte("."))
	mac.Write(got.body)
	replay := "v1=" + hex.EncodeToString(mac.Sum(nil))

	if got.signature == replay {
		t.Error("the signature does not depend on the timestamp")
	}
}

// One secret per source. A shared one would let any source holding it forge a
// signed callback to another.
func TestOneSourcesSecretDoesNotSignForAnother(t *testing.T) {
	n, w, got := callback(t, http.StatusOK)

	if err := n.Notify(context.Background(), w, batch()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	mac := hmac.New(sha256.New, []byte("somebody-elses-secret"))
	mac.Write([]byte(got.timestamp))
	mac.Write([]byte("."))
	mac.Write(got.body)

	if got.signature == "v1="+hex.EncodeToString(mac.Sum(nil)) {
		t.Error("another source's secret produced the same signature")
	}
}

// Sending unsigned would work, and that is the problem: a receiver would have
// nothing to check, and the missing secret would surface on the day somebody
// started checking rather than the day it was deployed.
func TestNothingIsSentUnsigned(t *testing.T) {
	n, w, got := callbackWith(t, http.StatusOK, secrets{})

	if err := n.Notify(context.Background(), w, batch()); err == nil {
		t.Fatal("Notify succeeded with no signing secret")
	}
	if got.seen {
		t.Error("an unsigned callback was posted anyway")
	}
}

func TestWhatCountsAsDelivered(t *testing.T) {
	tests := map[int]bool{
		http.StatusOK:                  true,
		http.StatusCreated:             true,
		http.StatusAccepted:            true,
		http.StatusNoContent:           true,
		http.StatusMovedPermanently:    false,
		http.StatusBadRequest:          false,
		http.StatusUnauthorized:        false,
		http.StatusInternalServerError: false,
	}

	for status, ok := range tests {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			n, w, _ := callback(t, status)

			err := n.Notify(context.Background(), w, batch())
			if ok && err != nil {
				t.Errorf("Notify = %v, want it accepted", err)
			}
			if !ok && err == nil {
				t.Error("Notify accepted a refusal")
			}
		})
	}
}

func TestAnEndpointWeCannotReach(t *testing.T) {
	n, w, _ := callback(t, http.StatusOK)
	w.CallbackURL = "http://127.0.0.1:1/hooks"

	if err := n.Notify(context.Background(), w, batch()); err == nil {
		t.Fatal("Notify succeeded against nothing")
	}
}

func TestANotifierRefusesToBeBuiltHalfWired(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := func() time.Time { return at }

	if _, err := notifier.New(nil, secrets{}, now, log); err == nil {
		t.Error("New with no client succeeded")
	}
	if _, err := notifier.New(http.DefaultClient, nil, now, log); err == nil {
		t.Error("New with no secrets succeeded")
	}
	if _, err := notifier.New(http.DefaultClient, secrets{}, nil, log); err == nil {
		t.Error("New with no clock succeeded")
	}
	if _, err := notifier.New(http.DefaultClient, secrets{}, now, nil); err == nil {
		t.Error("New with no logger succeeded")
	}
}

func TestThereIsNothingToPostTo(t *testing.T) {
	n, _, _ := callback(t, http.StatusOK)

	if err := n.Notify(context.Background(), nil, batch()); !errs.IsType(err, errs.ErrInternal) {
		t.Errorf("Notify(nil) = %v", err)
	}
}
