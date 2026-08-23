// This test is in the package rather than beside it because the order things
// close in is the behavior under test, and building that order means adding
// fake steps -- which is deliberately not part of the public surface.
package registry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Serajian/srosha/internal/config/settings"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func noop(context.Context) error { return nil }

func recorder(closed *[]string, name string) func(context.Context) error {
	return func(context.Context) error {
		*closed = append(*closed, name)
		return nil
	}
}

func assertOrder(t *testing.T, closed, want []string) {
	t.Helper()

	if len(closed) != len(want) {
		t.Fatalf("closed %v, want %v", closed, want)
	}
	for i := range want {
		if closed[i] != want[i] {
			t.Fatalf("closed %v, want %v", closed, want)
		}
	}
}

// The tier decides, not the order things were opened in. They go in here
// deliberately scrambled: if insertion order still decided, this would come out
// backwards.
func TestClosesFromTheOutsideIn(t *testing.T) {
	res := New(discard())

	var closed []string
	res.add(step{tier: tierClient, name: "http client", close: recorder(&closed, "http client")})
	res.add(step{tier: tierServer, name: "server", close: recorder(&closed, "server")})
	res.add(step{tier: tierStore, name: "postgres", close: recorder(&closed, "postgres")})
	res.add(step{tier: tierBroker, name: "nats", close: recorder(&closed, "nats")})

	if err := res.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	assertOrder(t, closed, []string{"server", "http client", "nats", "postgres"})
}

// Within one tier there is nothing to sort by, so they still unwind the way
// they were built.
func TestOneTierClosesInReverseOfOpening(t *testing.T) {
	res := New(discard())

	var closed []string
	res.add(step{tier: tierClient, name: "webhook", close: recorder(&closed, "webhook")})
	res.add(step{tier: tierClient, name: "sender", close: recorder(&closed, "sender")})

	if err := res.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	assertOrder(t, closed, []string{"sender", "webhook"})
}

// Every tier has to be walked. A dependency at a tier Close skips would be left
// open with nothing left to notice.
func TestNoTierIsSkipped(t *testing.T) {
	res := New(discard())

	var closed []string
	for tr := tierStore; tr <= tierHighest; tr++ {
		res.add(step{tier: tr, name: "x", close: recorder(&closed, "x")})
	}

	if err := res.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if want := int(tierHighest-tierStore) + 1; len(closed) != want {
		t.Fatalf("closed %d of %d tiers", len(closed), want)
	}
}

func TestCloseKeepsGoingPastAFailure(t *testing.T) {
	res := New(discard())

	poolClosed := false
	res.add(step{tier: tierStore, name: "postgres", close: func(context.Context) error {
		poolClosed = true
		return nil
	}})
	res.add(step{tier: tierBroker, name: "nats", close: func(context.Context) error {
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

// Ready reports each dependency separately: whoever asked has to know WHICH one
// is down, and reading it out of an error's text is exactly what is forbidden.
func TestReadyReportsEachDependencySeparately(t *testing.T) {
	res := New(discard())

	res.add(
		step{tier: tierStore, name: "postgres", close: noop, ready: func(context.Context) error {
			return errors.New("connection refused")
		}},
	)
	res.add(step{tier: tierBroker, name: "nats", close: noop, ready: noop})

	checks := res.Ready(context.Background())
	if len(checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(checks))
	}

	got := map[string]error{}
	for _, c := range checks {
		got[c.Name] = c.Err
	}
	if got["postgres"] == nil {
		t.Error("postgres answered but is down")
	}
	if got["nats"] != nil {
		t.Errorf("nats is up but reported %v", got["nats"])
	}
}

// One failure must not hide the next: an operator wants every name.
func TestReadyDoesNotStopAtTheFirstFailure(t *testing.T) {
	res := New(discard())

	for _, name := range []string{"postgres", "nats"} {
		res.add(step{tier: tierStore, name: name, close: noop, ready: func(context.Context) error {
			return errors.New("down")
		}})
	}

	down := 0
	for _, c := range res.Ready(context.Background()) {
		if c.Err != nil {
			down++
		}
	}
	if down != 2 {
		t.Errorf("%d reported down, want 2", down)
	}
}

// Something with no health of its own is not asked, and does not appear as a
// dependency that failed to answer.
func TestReadyIgnoresWhatHasNoHealth(t *testing.T) {
	res := New(discard())

	res.add(step{tier: tierClient, name: "http client", close: noop})

	if checks := res.Ready(context.Background()); len(checks) != 0 {
		t.Fatalf("got %d checks, want none", len(checks))
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
