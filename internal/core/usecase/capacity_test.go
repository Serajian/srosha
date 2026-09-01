package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/usecase"
)

const gib = uint64(1) << 30

// The whole point of the edge: an alert that repeats every fifteen minutes is
// one people learn to scroll past, and then the next one is scrolled past too.
func TestItSaysSoOnceAndSaysWhenItIsOverAgain(t *testing.T) {
	fs := &fakeFS{available: 10 * gib, total: 96 * gib}
	told := &recorder{}
	c := usecase.NewCapacity(fs, told, quiet(), "/", 5*gib)

	run := func() {
		t.Helper()
		if err := c.Check(context.Background()); err != nil {
			t.Fatalf("check: %v", err)
		}
	}

	run()
	if len(told.subjects) != 0 {
		t.Fatalf("above the floor and it spoke: %v", told.subjects)
	}

	fs.available = 4 * gib
	run()
	run()
	run()
	if len(told.subjects) != 1 {
		t.Fatalf("crossed once, said it %d times: %v", len(told.subjects), told.subjects)
	}
	if !strings.Contains(told.subjects[0], "running out") {
		t.Errorf("said %q, want it to say the disk is running out", told.subjects[0])
	}

	fs.available = 40 * gib
	run()
	run()
	if len(told.subjects) != 2 {
		t.Fatalf("recovered once, said it %d times: %v", len(told.subjects), told.subjects)
	}
	if !strings.Contains(told.subjects[1], "room again") {
		t.Errorf("said %q, want it to say there is room again", told.subjects[1])
	}

	// And it can cross again. A latch that only fires once is a check that
	// works until the first time it matters twice.
	fs.available = 1 * gib
	run()
	if len(told.subjects) != 3 {
		t.Fatalf("second crossing was not reported: %v", told.subjects)
	}
}

// The detail is what makes the alert actionable: this host is shared with
// dozens of unrelated applications, and the first question at three in the
// morning is whether the problem is ours.
func TestTheAlertSaysHowMuchOfTheDiskIsOurs(t *testing.T) {
	fs := &fakeFS{available: 1 * gib, total: 100 * gib}
	told := &recorder{}
	c := usecase.NewCapacity(fs, told, quiet(), "/", 5*gib)
	c.Watch("postgres", sized{2 * gib})
	c.Watch("jetstream", sized{gib / 2})

	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(told.details) != 1 {
		t.Fatalf("expected one alert, got %d", len(told.details))
	}

	d := told.details[0]
	for _, want := range []string{"postgres", "jetstream", "99.0 GB in use"} {
		if !strings.Contains(d, want) {
			t.Errorf("detail does not mention %q:\n%s", want, d)
		}
	}
}

// A store that cannot be reached must not silence the alert. The free space is
// the part somebody has to act on; our own share is context.
func TestAStoreThatCannotBeAskedStillLetsTheAlertThrough(t *testing.T) {
	told := &recorder{}
	c := usecase.NewCapacity(
		&fakeFS{available: 1 * gib, total: 100 * gib}, told, quiet(), "/", 5*gib,
	)
	c.Watch("postgres", broken{})

	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(told.details) != 1 {
		t.Fatalf("a broken store swallowed the alert")
	}
	if !strings.Contains(told.details[0], "postgres: unknown") {
		t.Errorf("want the detail to say the number is missing, got:\n%s", told.details[0])
	}
}

// Not being able to read the disk is a failure of the check itself, so it goes
// back to the scheduler rather than out as news about the disk.
func TestAnUnreadableFilesystemIsAnErrorAndNotAnAlert(t *testing.T) {
	told := &recorder{}
	c := usecase.NewCapacity(&fakeFS{err: errors.New("no")}, told, quiet(), "/", 5*gib)

	if err := c.Check(context.Background()); err == nil {
		t.Fatal("want an error when the filesystem cannot be read")
	}
	if len(told.subjects) != 0 {
		t.Errorf("it alerted on its own failure: %v", told.subjects)
	}
}

// --- fakes ------------------------------------------------------------------

type fakeFS struct {
	available, total uint64
	err              error
}

func (f *fakeFS) Free(string) (uint64, uint64, error) {
	return f.available, f.total, f.err
}

type recorder struct {
	subjects []string
	details  []string
}

func (r *recorder) Notify(_ context.Context, subject, detail string) {
	r.subjects = append(r.subjects, subject)
	r.details = append(r.details, detail)
}

type sized struct{ n uint64 }

func (s sized) Bytes(context.Context) (uint64, error) { return s.n, nil }

type broken struct{}

func (broken) Bytes(context.Context) (uint64, error) {
	return 0, errors.New("the store did not answer")
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
