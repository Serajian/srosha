package notification_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

var (
	testID  = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8G9H")
	testNow = time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
)

func testOrigin() notification.Origin {
	return notification.Origin{ID: "acme", Name: "Acme Payments", MaxPriority: shared.PriorityHigh}
}

func testRequest() notification.Request {
	return notification.Request{Body: "your order shipped", Priority: shared.PriorityNormal}
}

func TestNewAcceptsAValidRequest(t *testing.T) {
	req := testRequest()
	req.IdempotencyKey = "order-4471"
	req.Title = "Order shipped"
	req.Metadata = map[string]string{"order_id": "4471"}

	n, err := notification.New(testID, testOrigin(), req, testNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if n.ID != testID {
		t.Errorf("ID = %q, want %q", n.ID, testID)
	}
	if n.SourceID != "acme" || n.SourceName != "Acme Payments" {
		t.Errorf("origin not copied: %q / %q", n.SourceID, n.SourceName)
	}
	if n.IdempotencyKey != "order-4471" || n.Title != "Order shipped" {
		t.Errorf("request fields not copied")
	}
	if !n.CreatedAt.Equal(testNow) {
		t.Errorf("CreatedAt = %v, want %v", n.CreatedAt, testNow)
	}
	if got := n.Metadata()["order_id"]; got != "4471" {
		t.Errorf("Metadata()[order_id] = %q, want %q", got, "4471")
	}
}

// A priority above the source's ceiling is clamped, never rejected: a source's
// own configuration must not become a runtime error for its callers.
func TestNewClampsPriorityToTheCeiling(t *testing.T) {
	tests := []struct {
		name              string
		ceiling           shared.Priority
		requested         shared.Priority
		wantEffective     shared.Priority
		wantWasDowngraded bool
	}{
		{"below ceiling", shared.PriorityHigh, shared.PriorityNormal, shared.PriorityNormal, false},
		{"at ceiling", shared.PriorityHigh, shared.PriorityHigh, shared.PriorityHigh, false},
		{"above ceiling", shared.PriorityHigh, shared.PriorityCritical, shared.PriorityHigh, true},
		{"normal ceiling", shared.PriorityNormal, shared.PriorityCritical, shared.PriorityNormal, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := testOrigin()
			org.MaxPriority = tt.ceiling
			req := testRequest()
			req.Priority = tt.requested

			n, err := notification.New(testID, org, req, testNow)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if n.RequestedPriority != tt.requested {
				t.Errorf("RequestedPriority = %v, want %v", n.RequestedPriority, tt.requested)
			}
			if n.EffectivePriority != tt.wantEffective {
				t.Errorf("EffectivePriority = %v, want %v", n.EffectivePriority, tt.wantEffective)
			}
			if n.WasDowngraded() != tt.wantWasDowngraded {
				t.Errorf("WasDowngraded() = %v, want %v", n.WasDowngraded(), tt.wantWasDowngraded)
			}
		})
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	past := testNow.Add(-time.Hour)

	tests := []struct {
		name     string
		id       shared.ID
		org      notification.Origin
		mutate   func(*notification.Request)
		now      time.Time
		sentinel error
		typ      errs.Type
	}{
		{
			name: "missing id", id: "", org: testOrigin(), now: testNow,
			sentinel: shared.ErrInvalidID, typ: errs.ErrInternal,
		},
		{
			name: "missing source", id: testID, org: notification.Origin{}, now: testNow,
			sentinel: notification.ErrMissingSource, typ: errs.ErrInternal,
		},
		{
			name: "missing timestamp", id: testID, org: testOrigin(), now: time.Time{},
			sentinel: notification.ErrMissingTimestamp, typ: errs.ErrInternal,
		},
		{
			name: "empty body", id: testID, org: testOrigin(), now: testNow,
			mutate:   func(r *notification.Request) { r.Body = "" },
			sentinel: notification.ErrEmptyBody, typ: errs.ErrInvalidInput,
		},
		{
			name: "unknown priority", id: testID, org: testOrigin(), now: testNow,
			mutate:   func(r *notification.Request) { r.Priority = shared.Priority(42) },
			sentinel: shared.ErrUnknownPriority, typ: errs.ErrInvalidInput,
		},
		{
			name: "expiry in the past", id: testID, org: testOrigin(), now: testNow,
			mutate:   func(r *notification.Request) { r.ExpireAt = &past },
			sentinel: notification.ErrAlreadyExpired, typ: errs.ErrInvalidInput,
		},
		{
			name: "expiry equal to now", id: testID, org: testOrigin(), now: testNow,
			mutate:   func(r *notification.Request) { r.ExpireAt = &testNow },
			sentinel: notification.ErrAlreadyExpired, typ: errs.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testRequest()
			if tt.mutate != nil {
				tt.mutate(&req)
			}

			n, err := notification.New(tt.id, tt.org, req, tt.now)
			if err == nil {
				t.Fatalf("New() = %+v, want an error", n)
			}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("errors.Is(%v) = false, want the sentinel", tt.sentinel)
			}
			if !errs.IsType(err, tt.typ) {
				t.Errorf("type = %v, want %v", errs.TypeOf(err), tt.typ)
			}
		})
	}
}

// Our own missing values are internal errors, never the caller's fault: the
// service generates the id and injects the clock, so an empty one is a bug on
// our side and must not read to a client as something they could fix.
func TestOurOwnMissingValuesAreInternal(t *testing.T) {
	_, err := notification.New("", testOrigin(), testRequest(), testNow)
	if errs.IsType(err, errs.ErrInvalidInput) {
		t.Error("a missing generated id must not be reported as invalid input")
	}
}

// The caller keeps no handle on aggregate state: changing the map they passed
// in must not change the notification.
func TestMetadataIsCopiedOnTheWayIn(t *testing.T) {
	md := map[string]string{"env": "test"}
	req := testRequest()
	req.Metadata = md

	n, err := notification.New(testID, testOrigin(), req, testNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	md["env"] = "production"
	md["leaked"] = "yes"

	if got := n.Metadata()["env"]; got != "test" {
		t.Errorf("Metadata()[env] = %q, want %q", got, "test")
	}
	if _, ok := n.Metadata()["leaked"]; ok {
		t.Error("a key added after construction reached the notification")
	}
}

// And nothing leaks out either: the map Metadata hands back is a copy.
func TestMetadataIsCopiedOnTheWayOut(t *testing.T) {
	req := testRequest()
	req.Metadata = map[string]string{"env": "test"}

	n, err := notification.New(testID, testOrigin(), req, testNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	n.Metadata()["env"] = "production"

	if got := n.Metadata()["env"]; got != "test" {
		t.Errorf("Metadata()[env] = %q, want %q", got, "test")
	}
}

func TestMetadataIsNilWhenNoneWasSupplied(t *testing.T) {
	n, err := notification.New(testID, testOrigin(), testRequest(), testNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if n.Metadata() != nil {
		t.Errorf("Metadata() = %v, want nil", n.Metadata())
	}
}

func TestIsExpired(t *testing.T) {
	later := testNow.Add(time.Hour)

	tests := []struct {
		name     string
		expireAt *time.Time
		at       time.Time
		want     bool
	}{
		{"no deadline", nil, testNow.Add(100 * time.Hour), false},
		{"before deadline", &later, testNow, false},
		{"at deadline", &later, later, true},
		{"after deadline", &later, later.Add(time.Second), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testRequest()
			req.ExpireAt = tt.expireAt

			n, err := notification.New(testID, testOrigin(), req, testNow)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := n.IsExpired(tt.at); got != tt.want {
				t.Errorf("IsExpired(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

// Restore must load a row the current constructor would reject. A rule that
// tightens tomorrow must not make yesterday's rows unreadable -- otherwise a
// validation change becomes an outage on historical data.
func TestRestoreLoadsARowNewWouldReject(t *testing.T) {
	stored := notification.Notification{
		ID:       testID,
		SourceID: "acme",
		Body:     "", // New refuses this
	}

	n := notification.Restore(stored, map[string]string{"order_id": "4471"})

	if n.ID != testID || n.SourceID != "acme" {
		t.Errorf("exported fields not carried through")
	}
	if got := n.Metadata()["order_id"]; got != "4471" {
		t.Errorf("Metadata()[order_id] = %q, want %q", got, "4471")
	}
}

func TestRestoreCopiesMetadata(t *testing.T) {
	md := map[string]string{"env": "test"}
	n := notification.Restore(notification.Notification{ID: testID}, md)

	md["env"] = "production"

	if got := n.Metadata()["env"]; got != "test" {
		t.Errorf("Metadata()[env] = %q, want %q", got, "test")
	}
}
