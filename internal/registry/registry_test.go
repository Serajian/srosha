// This test is in the package rather than beside it because the order things
// close in is the behavior under test, and building that order means adding
// fake steps -- which is deliberately not part of the public surface.
package registry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/config/settings"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func noop(context.Context) error { return nil }

func TestClosesInReverseOfOpening(t *testing.T) {
	res := New(discard())

	var closed []string
	record := func(name string) func(context.Context) error {
		return func(context.Context) error {
			closed = append(closed, name)
			return nil
		}
	}

	res.add(step{name: "postgres", close: record("postgres")})
	res.add(step{name: "nats", close: record("nats")})
	res.add(step{name: "consumer", close: record("consumer")})

	if err := res.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	want := []string{"consumer", "nats", "postgres"}
	for i := range want {
		if closed[i] != want[i] {
			t.Fatalf("closed %v, want %v", closed, want)
		}
	}
}

func TestCloseKeepsGoingPastAFailure(t *testing.T) {
	res := New(discard())

	poolClosed := false
	res.add(step{name: "postgres", close: func(context.Context) error {
		poolClosed = true
		return nil
	}})
	res.add(step{name: "nats", close: func(context.Context) error {
		return errors.New("drain timed out")
	}})

	if err := res.Close(context.Background()); err == nil {
		t.Fatal("want the nats failure reported")
	}
	if !poolClosed {
		t.Fatal("a broker that refuses to drain must not leave the pool open")
	}
}

func TestCloseTwiceIsSafe(t *testing.T) {
	res := New(discard())

	calls := 0
	res.add(step{name: "postgres", close: func(context.Context) error {
		calls++
		return nil
	}})

	ctx := context.Background()
	if err := res.Close(ctx); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := res.Close(ctx); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if calls != 1 {
		t.Fatalf("closed %d times, want 1", calls)
	}
}

func TestReadyNamesEveryDependencyThatIsDown(t *testing.T) {
	res := New(discard())

	res.add(step{name: "postgres", close: noop, ready: func(context.Context) error {
		return errors.New("connection refused")
	}})
	res.add(step{name: "nats", close: noop, ready: func(context.Context) error {
		return errors.New("no servers available")
	}})

	err := res.Ready(context.Background())
	if err == nil {
		t.Fatal("want both failures reported")
	}
	for _, name := range []string{"postgres", "nats"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("%q does not name %s", err, name)
		}
	}
}

func TestReadyIgnoresWhatHasNoHealth(t *testing.T) {
	res := New(discard())

	res.add(step{name: "http client", close: noop})

	if err := res.Ready(context.Background()); err != nil {
		t.Fatalf("want ready, got %v", err)
	}
}

// Postgres is not reachable without a database, so this holds the one thing
// that fails before any I/O: a config the infra package refuses.
func TestPostgresRefusesAnEmptyDSN(t *testing.T) {
	res := New(discard())

	if _, err := Postgres(context.Background(), settings.DB{}, res); err == nil {
		t.Fatal("want an empty dsn refused")
	}
	if len(res.steps) != 0 {
		t.Fatal("a failed open must not leave a step behind")
	}
}

// NATS is not reachable without a broker, so this holds the one thing that
// fails before any I/O: a config the infra package refuses.
func TestNATSRefusesAnEmptyURL(t *testing.T) {
	res := New(discard())

	if _, err := NATS(context.Background(), settings.MQ{}, res); err == nil {
		t.Fatal("want an empty url refused")
	}
	if len(res.steps) != 0 {
		t.Fatal("a failed open must not leave a step behind")
	}
}
