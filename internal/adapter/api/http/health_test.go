package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	srhttp "github.com/Serajian/srosha/internal/adapter/api/http"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var errRefused = errors.New("dial tcp 10.0.0.5:5432: connection refused")

func serving(t *testing.T, binary string, checks ...srhttp.Check) nethttp.Handler {
	t.Helper()

	h, err := srhttp.New(srhttp.Deps{
		Binary: binary,
		Ready:  func(context.Context) []srhttp.Check { return checks },
		Log:    discard(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return h
}

func get(t *testing.T, h nethttp.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, path, nil))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not json: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestNewRefusesIncompleteDeps(t *testing.T) {
	ready := func(context.Context) []srhttp.Check { return nil }

	tests := []struct {
		name string
		deps srhttp.Deps
		want string
	}{
		{"no binary", srhttp.Deps{Ready: ready, Log: discard()}, "binary"},
		{"no readiness", srhttp.Deps{Binary: "gateway", Log: discard()}, "readiness"},
		{"no logger", srhttp.Deps{Binary: "gateway", Ready: ready}, "logger"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := srhttp.New(tt.deps); err == nil {
				t.Fatal("New() accepted it")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not say what is wrong: %v", err)
			}
		})
	}
}

// Liveness must not depend on anything: restarting the process does not bring
// postgres back, and a restart loop buries the real fault.
func TestLivenessAnswersEvenWhenNothingIsReady(t *testing.T) {
	h := serving(t, "gateway", srhttp.Check{Name: "postgres", Err: errRefused})

	if rec := get(t, h, "/healthz"); rec.Code != nethttp.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestReadinessFollowsWhatIsOpen(t *testing.T) {
	up := serving(t, "gateway", srhttp.Check{Name: "postgres"}, srhttp.Check{Name: "nats"})
	if rec := get(t, up, "/readyz"); rec.Code != nethttp.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	down := serving(t, "gateway",
		srhttp.Check{Name: "postgres", Err: errRefused}, srhttp.Check{Name: "nats"})
	if rec := get(t, down, "/readyz"); rec.Code != nethttp.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// The whole point of reporting per dependency: an operator has to see WHICH one
// is down without opening the log.
func TestReadinessNamesTheOneThatIsDown(t *testing.T) {
	h := serving(t, "dispatcher",
		srhttp.Check{Name: "postgres", Err: errRefused}, srhttp.Check{Name: "nats"})

	body := decode(t, get(t, h, "/readyz"))

	if body["binary"] != "dispatcher" {
		t.Errorf("binary = %v, want dispatcher", body["binary"])
	}
	if body["status"] != "not ready" {
		t.Errorf("status = %v, want not ready", body["status"])
	}

	got := map[string]string{}
	for _, raw := range body["checks"].([]any) {
		c := raw.(map[string]any)
		got[c["name"].(string)] = c["status"].(string)
	}
	if got["postgres"] != "down" {
		t.Errorf("postgres = %q, want down", got["postgres"])
	}
	if got["nats"] != "up" {
		t.Errorf("nats = %q, want up", got["nats"])
	}
}

// Naming the dependency is not the same as publishing the reason. This port is
// reachable from further away than its author expects, and the reason carries
// our internal addresses.
func TestReadinessDoesNotPublishTheReason(t *testing.T) {
	h := serving(t, "gateway", srhttp.Check{Name: "postgres", Err: errRefused})

	body := get(t, h, "/readyz").Body.String()
	for _, leak := range []string{"10.0.0.5", "connection refused", "dial tcp"} {
		if strings.Contains(body, leak) {
			t.Errorf("the body carries %q: %s", leak, body)
		}
	}
}

// A binary with nothing that has health of its own is ready, not broken.
func TestNoChecksIsReady(t *testing.T) {
	rec := get(t, serving(t, "gateway"), "/readyz")

	if rec.Code != nethttp.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if decode(t, rec)["status"] != "ready" {
		t.Errorf("status = %v, want ready", decode(t, rec)["status"])
	}
}

func TestOnlyGETIsServed(t *testing.T) {
	h := serving(t, "gateway")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodPost, "/readyz", nil))

	if rec.Code == nethttp.StatusOK {
		t.Error("POST /readyz was served")
	}
}
