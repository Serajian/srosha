package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Serajian/srosha/internal/registry"
)

// spy records what it was told, in order.
type spy struct{ said []string }

func (s *spy) Notify(_ context.Context, subject, _ string) {
	s.said = append(s.said, subject)
}

// Alerts fire on the change, not on the state.
//
// A database that is down for ten minutes is one message, not one every thirty
// seconds. The second kind trains an operator to ignore the first, which is how
// alerting dies.
func TestOnlyTransitionsAlert(t *testing.T) {
	down := errors.New("gone")

	rounds := [][]registry.Check{
		{{Name: "postgres"}},            // up
		{{Name: "postgres", Err: down}}, // fell over    -> one alert
		{{Name: "postgres", Err: down}}, // still down   -> silence
		{{Name: "postgres", Err: down}}, // still down   -> silence
		{{Name: "postgres"}},            // came back    -> one alert
		{{Name: "postgres"}},            // still up     -> silence
	}

	var told spy
	w := newWatcher(&told, quietLog())
	for _, r := range rounds {
		w.compare(context.Background(), r)
	}

	if len(told.said) != 2 {
		t.Fatalf("said %d things, want 2: %v", len(told.said), told.said)
	}
}

// The first look is not a transition. A binary that starts with a dependency
// already down has said so through the startup alert; repeating it here would
// double every restart.
func TestTheFirstLookIsSilent(t *testing.T) {
	var told spy
	w := newWatcher(&told, quietLog())

	w.compare(context.Background(), []registry.Check{{Name: "schema", Err: errors.New("behind")}})

	if len(told.said) != 0 {
		t.Errorf("the first look alerted: %v", told.said)
	}
}

// Each dependency is watched separately, so postgres going down does not hide
// nats coming back.
func TestEachDependencyIsItsOwn(t *testing.T) {
	down := errors.New("gone")

	var told spy
	w := newWatcher(&told, quietLog())

	w.compare(context.Background(), []registry.Check{{Name: "postgres"}, {Name: "nats"}})
	w.compare(context.Background(), []registry.Check{
		{Name: "postgres", Err: down},
		{Name: "nats"},
	})
	w.compare(context.Background(), []registry.Check{
		{Name: "postgres", Err: down},
		{Name: "nats", Err: down},
	})

	if len(told.said) != 2 {
		t.Fatalf("said %d things, want one per dependency: %v", len(told.said), told.said)
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
