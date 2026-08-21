package health_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/api/health"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// Liveness must not depend on anything: restarting the process does not bring
// postgres back, and a restart loop buries the real fault.
func TestLivenessAnswersEvenWhenNothingIsReady(t *testing.T) {
	down := func(context.Context) error { return errors.New("postgres: connection refused") }

	rec := get(t, health.Handler(down, discard()), "/healthz")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestReadinessFollowsWhatIsOpen(t *testing.T) {
	up := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("postgres: connection refused") }

	if rec := get(t, health.Handler(up, discard()), "/readyz"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec := get(t, health.Handler(down, discard()), "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// The reason names our dependencies and how they failed. This port can be
// reachable from further away than its author expects, so the reason goes to
// the log and the body says only that we are not ready.
func TestReadinessDoesNotPublishTheReason(t *testing.T) {
	down := func(context.Context) error {
		return errors.New("postgres: dial tcp 10.0.0.5:5432: connection refused")
	}

	rec := get(t, health.Handler(down, discard()), "/readyz")

	body := rec.Body.String()
	for _, leak := range []string{"postgres", "10.0.0.5", "connection refused"} {
		if strings.Contains(body, leak) {
			t.Errorf("the body carries %q: %s", leak, body)
		}
	}
}

func TestOnlyGETIsServed(t *testing.T) {
	up := func(context.Context) error { return nil }
	h := health.Handler(up, discard())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/readyz", nil))

	if rec.Code == http.StatusOK {
		t.Error("POST /readyz was served")
	}
}
