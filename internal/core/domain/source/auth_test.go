package source_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

var (
	keyID   = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8K01")
	authNow = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
)

// fakeKeys answers the way the statement does: no live key is a nil Source, not
// an error. Everything about telling revoked from expired from unknown happens
// inside the WHERE clause, so there is nothing here to tell apart either.
type fakeKeys struct {
	src *source.Source
	err error

	touched   shared.ID
	touchedAt time.Time
	touchErr  error
}

func (f *fakeKeys) ReadSourceByKeyHash(
	_ context.Context, _ string, _ time.Time,
) (*source.Source, shared.ID, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	if f.src == nil {
		return nil, "", nil
	}
	return f.src, keyID, nil
}

func (f *fakeKeys) Touch(
	_ context.Context, id shared.ID, now time.Time, _ time.Duration,
) error {
	f.touched, f.touchedAt = id, now
	return f.touchErr
}

func anAuthenticator(keys *fakeKeys) *source.Authenticator {
	return source.NewAuthenticator(keys, func() time.Time { return authNow }, time.Hour)
}

func aLiveSource(active bool) *source.Source {
	return &source.Source{
		ID: "01J8XQ2M4E7N9V3B5C6D7F8S01", Name: "Acme",
		MaxPriority: shared.PriorityHigh, IsActive: active,
	}
}

func TestAuthenticateFindsTheCaller(t *testing.T) {
	keys := &fakeKeys{src: aLiveSource(true)}

	got, id, err := anAuthenticator(keys).Authenticate(context.Background(), "somehash")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got.ID != keys.src.ID {
		t.Errorf("ID = %q, want %q", got.ID, keys.src.ID)
	}
	if id != keyID {
		t.Errorf("key id = %q, want %q -- RecordUse has nothing to touch without it", id, keyID)
	}
}

// Revoked, expired and never-existed all arrive as no row, and all three must
// come back out as one answer. Telling them apart tells whoever is guessing
// which of their strings was once real.
func TestEveryWayOfNotBeingAKeyLooksTheSame(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"unknown, revoked or expired -- the statement cannot say which", "somehash"},
		{"nothing presented at all", ""},
	}

	var messages []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := anAuthenticator(&fakeKeys{}).Authenticate(context.Background(), tt.hash)

			if !errors.Is(err, source.ErrUnknownKey) {
				t.Fatalf("error = %v, want ErrUnknownKey", err)
			}
			if !errs.IsType(err, errs.ErrUnauthorized) {
				t.Errorf("type = %v, want unauthorized", errs.TypeOf(err))
			}
			messages = append(messages, err.Error())
		})
	}

	if messages[0] != messages[1] {
		t.Errorf("the two answers differ:\n  %q\n  %q", messages[0], messages[1])
	}
}

// A suspended source is the one case that must NOT look like a bad key. The key
// is genuine and the account is off, and a customer told "invalid credentials"
// spends the outage rotating a key that was never the problem.
func TestASuspendedSourceIsForbiddenNotUnauthenticated(t *testing.T) {
	keys := &fakeKeys{src: aLiveSource(false)}

	_, _, err := anAuthenticator(keys).Authenticate(context.Background(), "somehash")

	if !errors.Is(err, source.ErrSourceInactive) {
		t.Fatalf("error = %v, want ErrSourceInactive", err)
	}
	if !errs.IsType(err, errs.ErrForbidden) {
		t.Errorf("type = %v, want forbidden", errs.TypeOf(err))
	}
}

// A database that is down is not a bad key either: reporting it as one would
// have every customer rotating keys during our outage.
func TestALookupFailureIsNotABadKey(t *testing.T) {
	down := errors.New("connection refused")

	_, _, err := anAuthenticator(&fakeKeys{err: down}).
		Authenticate(context.Background(), "somehash")

	if !errors.Is(err, down) {
		t.Fatalf("error = %v, want the lookup failure itself", err)
	}
	if errors.Is(err, source.ErrUnknownKey) {
		t.Error("an outage was reported as invalid credentials")
	}
}

func TestRecordUseTouchesTheKeyThatWasUsed(t *testing.T) {
	keys := &fakeKeys{src: aLiveSource(true)}

	if err := anAuthenticator(keys).RecordUse(context.Background(), keyID); err != nil {
		t.Fatalf("RecordUse() error = %v", err)
	}
	if keys.touched != keyID {
		t.Errorf("touched %q, want %q", keys.touched, keyID)
	}
	if !keys.touchedAt.Equal(authNow) {
		t.Errorf("touched at %v, want the injected clock %v", keys.touchedAt, authNow)
	}
}

// RecordUse is a separate call precisely so that its failure cannot reach
// Authenticate. This is what keeps bookkeeping from refusing a request.
func TestAFailedTouchCannotReachAuthenticate(t *testing.T) {
	keys := &fakeKeys{src: aLiveSource(true), touchErr: errors.New("write failed")}

	if _, _, err := anAuthenticator(keys).Authenticate(context.Background(), "h"); err != nil {
		t.Fatalf("Authenticate() error = %v, want nil", err)
	}
	if keys.touched != "" {
		t.Error("Authenticate touched the key itself, so a failed write could refuse a request")
	}
}
