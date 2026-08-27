package scheduler_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/scheduler"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()

	s, err := scheduler.New(
		scheduler.Config{Location: time.UTC, StopTimeout: 5 * time.Second},
		quiet(),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}

func TestAJobRunsOnItsSchedule(t *testing.T) {
	s := newScheduler(t)

	var runs atomic.Int64
	if err := s.Add("tick", scheduler.Every+"1s", func(context.Context) error {
		runs.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	time.Sleep(2500 * time.Millisecond)
	if got := runs.Load(); got < 2 {
		t.Errorf("ran %d times in 2.5s of a 1s schedule", got)
	}
}

// A rolling deploy brings every replica up within seconds of each other. A job
// that ran on boot would have all of them sweeping the same work at once.
func TestNothingRunsAtStartup(t *testing.T) {
	s := newScheduler(t)

	var runs atomic.Int64
	if err := s.Add("tick", scheduler.Every+"1h", func(context.Context) error {
		runs.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	time.Sleep(50 * time.Millisecond)
	if got := runs.Load(); got != 0 {
		t.Errorf("ran %d times before its first scheduled moment", got)
	}
}

// Two sweeps of the same work at once is how one job becomes two of everything
// it does.
func TestALongRunIsNotJoinedByTheNextOne(t *testing.T) {
	s := newScheduler(t)

	var running, overlaps atomic.Int64
	if err := s.Add("slow", scheduler.Every+"1s", func(context.Context) error {
		if running.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(1500 * time.Millisecond)
		running.Add(-1)
		return nil
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	time.Sleep(4 * time.Second)
	if got := overlaps.Load(); got != 0 {
		t.Errorf("%d runs started while another was going", got)
	}
}

// A failing sweep is not a reason to stop sweeping: whatever it did not get to
// is still there next time.
func TestAFailedRunDoesNotStopTheSchedule(t *testing.T) {
	s := newScheduler(t)

	var runs atomic.Int64
	if err := s.Add("failing", scheduler.Every+"1s", func(context.Context) error {
		runs.Add(1)
		return context.DeadlineExceeded
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	time.Sleep(2500 * time.Millisecond)
	if got := runs.Load(); got < 2 {
		t.Errorf("ran %d times, want it to keep going after a failure", got)
	}
}

func TestShutdownLetsARunFinish(t *testing.T) {
	s := newScheduler(t)

	finished := make(chan struct{})
	started := make(chan struct{})
	if err := s.Add("slow", scheduler.Every+"1s", func(context.Context) error {
		select {
		case <-started:
		default:
			close(started)
		}
		time.Sleep(300 * time.Millisecond)
		close(finished)
		return nil
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-started

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-finished:
	default:
		t.Error("Shutdown returned while a run was still going")
	}
}

// A job that will not stop must not hold the process for ever.
func TestShutdownGivesUp(t *testing.T) {
	s := newScheduler(t)

	if err := s.Add("stuck", scheduler.Every+"1s", func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	time.Sleep(1200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Shutdown(ctx) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Shutdown never returned")
	}
}

// Shutdown paths cross, so calling it twice has to be safe.
func TestShutdownIsSafeTwice(t *testing.T) {
	s := newScheduler(t)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("second Shutdown() = %v", err)
	}
}

// A scheduler is alive from the moment it is built, with goroutines of its own,
// so a startup that fails after this was created still has to close it.
func TestShutdownWorksOnOneThatNeverStarted(t *testing.T) {
	s := newScheduler(t)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() before Start = %v", err)
	}
	if err := s.Start(context.Background()); err == nil {
		t.Error("Start() after Shutdown succeeded, and it would never fire")
	}
}

// A typo in a schedule is found at startup, not at three in the morning.
func TestAnUnusableScheduleIsRefused(t *testing.T) {
	s := newScheduler(t)

	for _, bad := range []string{"", "every 5m", "*/5 * * *", "@sometimes", "0 99 * * *"} {
		if err := s.Add("job", bad, func(context.Context) error { return nil }); err == nil {
			t.Errorf("Add(%q) succeeded", bad)
		}
	}
}

// The same parser reads all three, which is why a schedule is a string.
func TestIntervalsAndCronSpecsBothParse(t *testing.T) {
	s := newScheduler(t)

	for _, good := range []string{"@every 5m", "*/5 * * * *", "0 3 * * *", "@daily"} {
		if err := s.Add("job", good, func(context.Context) error { return nil }); err != nil {
			t.Errorf("Add(%q) = %v", good, err)
		}
	}
}

func TestAJobNeedsANameAndSomethingToRun(t *testing.T) {
	s := newScheduler(t)

	if err := s.Add("  ", "@every 5m", func(context.Context) error { return nil }); err == nil {
		t.Error("Add with no name succeeded")
	}
	if err := s.Add("job", "@every 5m", nil); err == nil {
		t.Error("Add with no job succeeded")
	}
}

func TestASchedulerRefusesToBeBuiltHalfWired(t *testing.T) {
	if _, err := scheduler.New(scheduler.Config{StopTimeout: time.Second}, quiet()); err == nil {
		t.Error("New with no location succeeded")
	}
	if _, err := scheduler.New(scheduler.Config{Location: time.UTC}, quiet()); err == nil {
		t.Error("New with no stop timeout succeeded")
	}
	if _, err := scheduler.New(scheduler.Config{Location: time.UTC, StopTimeout: 5 * time.Second}, nil); err == nil {
		t.Error("New with no logger succeeded")
	}
}

func TestStartingTwiceIsAMistake(t *testing.T) {
	s := newScheduler(t)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	if err := s.Start(context.Background()); err == nil {
		t.Error("Start() twice succeeded")
	}
}
