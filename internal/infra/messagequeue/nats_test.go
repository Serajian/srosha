package messagequeue_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/messagequeue"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// nowhere points at a port nothing listens on, so connecting always fails.
func nowhere() messagequeue.Config {
	return messagequeue.Config{
		URL:            "nats://gateway:hunter2@127.0.0.1:1",
		ConnectTimeout: 200 * time.Millisecond,
		MaxReconnects:  -1,
		ReconnectWait:  10 * time.Millisecond,
		DrainTimeout:   200 * time.Millisecond,
	}
}

// New checks the wiring and dials nothing, so a mistake is found before any
// I/O happens.
func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	tests := []struct {
		name   string
		breaks func(*messagequeue.Config)
		want   string
	}{
		{"no url", func(c *messagequeue.Config) { c.URL = " " }, "url"},
		{
			"no connect timeout",
			func(c *messagequeue.Config) { c.ConnectTimeout = 0 },
			"connect timeout",
		},
		{
			"reconnects below the forever sentinel",
			func(c *messagequeue.Config) { c.MaxReconnects = -2 }, "max reconnects",
		},
		{
			"no reconnect wait",
			func(c *messagequeue.Config) { c.ReconnectWait = 0 },
			"reconnect wait",
		},
		{"no drain timeout", func(c *messagequeue.Config) { c.DrainTimeout = 0 }, "drain timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := nowhere()
			tt.breaks(&cfg)

			_, err := messagequeue.New(cfg, discard())
			if err == nil {
				t.Fatal("New() accepted it")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not say what is wrong: %v", err)
			}
		})
	}
}

// Zero reconnects is a deliberate choice, not a mistake: it says give up the
// moment the broker goes away.
func TestNewAcceptsReconnectingTurnedOff(t *testing.T) {
	cfg := nowhere()
	cfg.MaxReconnects = 0

	if _, err := messagequeue.New(cfg, discard()); err != nil {
		t.Fatalf("New() refused it: %v", err)
	}
}

// Every problem at once, so one restart fixes all of them.
func TestNewReportsEveryProblemTogether(t *testing.T) {
	_, err := messagequeue.New(messagequeue.Config{}, discard())
	if err == nil {
		t.Fatal("New() accepted an empty config")
	}
	for _, want := range []string{"url", "connect timeout", "reconnect wait", "drain timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// A driver error quotes the string it was given, and that string holds the
// password.
func TestConnectDoesNotLeakTheURL(t *testing.T) {
	cfg := nowhere()
	cfg.URL = "nats://gateway:hunter2@:::"

	n, err := messagequeue.New(cfg, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = n.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() accepted a malformed url")
	}
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), cfg.URL) {
		t.Errorf("the error carries the url: %v", err)
	}
}

func TestPingBeforeConnect(t *testing.T) {
	n, err := messagequeue.New(nowhere(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := n.Ping(context.Background()); err == nil {
		t.Error("Ping() succeeded before Connect()")
	}
	if n.Conn() != nil || n.JetStream() != nil {
		t.Error("a handle was given out before Connect()")
	}
}

// Shutdown paths cross, and the second caller must not hang or panic.
func TestDrainIsSafeTwiceAndBeforeConnect(t *testing.T) {
	n, err := messagequeue.New(nowhere(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := n.Drain(ctx); err != nil {
		t.Errorf("first drain: %v", err)
	}
	if err := n.Drain(ctx); err != nil {
		t.Errorf("second drain: %v", err)
	}
}

// nats keeps retrying underneath, so the wait is the one thing that decides
// when to give up. A canceled context must end it at once.
func TestACanceledContextStopsTheWait(t *testing.T) {
	cfg := nowhere()
	cfg.ConnectTimeout = 30 * time.Second
	cfg.ReconnectWait = time.Second

	n, err := messagequeue.New(cfg, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if err := n.Connect(ctx); err == nil {
		t.Fatal("Connect() succeeded against nothing")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v: the wait kept going after the context was canceled", took)
	}
}

// A failed Connect must leave nothing half-open behind it.
func TestAFailedConnectLeavesNoConnection(t *testing.T) {
	n, err := messagequeue.New(nowhere(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := n.Connect(context.Background()); err == nil {
		t.Fatal("Connect() succeeded against nothing")
	}
	if n.Conn() != nil || n.JetStream() != nil {
		t.Error("a handle was left behind after a failed connect")
	}
}

// A seed that is not one has to be caught before any dialing, and its own
// text must not appear in the error: the seed is the credential.
func TestABadNkeySeedIsRefusedWithoutRepeatingIt(t *testing.T) {
	cfg := nowhere()
	cfg.NkeySeed = "SUNOTAREALSEEDATALL"

	n, err := messagequeue.New(cfg, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = n.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() accepted a seed that is not one")
	}
	if strings.Contains(err.Error(), cfg.NkeySeed) {
		t.Errorf("the error carries the seed: %v", err)
	}
}

// No seed is the ordinary case until the rollout finishes, and it must behave
// exactly as it did before this option existed: the url authenticates, and the
// only reason the connection fails here is that nothing is listening.
func TestNoSeedLeavesTheUrlDoingTheWork(t *testing.T) {
	n, err := messagequeue.New(nowhere(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = n.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() reached a port nothing listens on")
	}
	if strings.Contains(err.Error(), "seed") {
		t.Errorf("a config with no seed failed for a seed reason: %v", err)
	}
}
