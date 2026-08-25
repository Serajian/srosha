// Package scheduler runs jobs on a schedule and owns their lifecycle. It knows
// nothing about what they do.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// Config is what this package needs. It is not the service's settings type:
// infra should stay copyable into another service, and mapping one to the other
// is registry's job.
type Config struct {
	// Location is what a schedule's clock means.
	//
	// UTC unless a deployment says otherwise, and that is not a preference. A
	// spec like "0 3 * * *" names a different moment in every zone, and in a
	// zone with daylight saving it names an hour that happens twice on one day
	// of the year and not at all on another.
	Location *time.Location

	// StopTimeout bounds how long a run in flight is waited for. Past it the
	// shutdown carries on rather than holding the whole process hostage to one
	// job that will not return.
	StopTimeout time.Duration
}

func (c Config) validate() error {
	var errs []error

	if c.Location == nil {
		errs = append(errs, errors.New("no location"))
	}
	if c.StopTimeout <= 0 {
		errs = append(errs, errors.New("stop timeout must be above zero"))
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("scheduler: %w", errors.Join(errs...))
}

// Scheduler holds the jobs and the clock that fires them.
type Scheduler struct {
	log   *slog.Logger
	cron  gocron.Scheduler
	limit gocron.LimitMode

	// running is the context every job inherits. It outlives the one startup
	// was given -- that one is done the moment the process is up -- and is
	// canceled only once a shutdown has waited as long as it is going to.
	running context.Context
	cancel  context.CancelFunc

	started bool
	closed  bool
}

// New checks the configuration and fires nothing. Add and Start do that, so a
// schedule that will not parse is a startup failure rather than a job that
// silently never runs.
//
// slog satisfies the library's logger as it is -- Debug, Error, Info and Warn
// with the same shape -- so the library's own messages land where ours do,
// under the same service and binary keys, with no adapter in between.
func New(cfg Config, log *slog.Logger) (*Scheduler, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if log == nil {
		return nil, errors.New("scheduler: no logger")
	}

	c, err := gocron.NewScheduler(
		gocron.WithLocation(cfg.Location),
		gocron.WithStopTimeout(cfg.StopTimeout),
		gocron.WithLogger(log),
	)
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}

	return &Scheduler{log: log, cron: c, limit: gocron.LimitModeReschedule}, nil
}

// Add registers one job. The schedule is parsed now, so a typo is found at
// startup and not at three in the morning.
//
// One spec form for everything: "@every 5m", "*/5 * * * *", "0 3 * * *" and
// "@daily" all go through the same parser, so a deployment that outgrows an
// interval needs no new setting. Seconds are not a field -- a schedule this
// service needs to the second is a schedule that wants a queue instead.
//
// A job that returns an error is logged and left to its next run. Whatever it
// did not finish is still there, which is the whole point of running on a
// schedule rather than once.
func (s *Scheduler) Add(name, schedule string, job func(context.Context) error) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("scheduler: job has no name")
	case job == nil:
		return fmt.Errorf("scheduler: job %q has nothing to run", name)
	}

	_, err := s.cron.NewJob(
		gocron.CronJob(schedule, false),
		gocron.NewTask(func() {
			if err := job(s.running); err != nil {
				s.log.ErrorContext(s.running, "scheduled job failed", "job", name, "err", err)
			}
		}),
		gocron.WithName(name),

		// A run still going when the next one is due is rescheduled rather than
		// started alongside it. Two sweeps of the same work at once is how one
		// job becomes two of everything it does.
		gocron.WithSingletonMode(s.limit),
	)
	if err != nil {
		return fmt.Errorf("scheduler: job %q has an unusable schedule %q: %w", name, schedule, err)
	}

	s.log.Info("scheduled", "job", name, "schedule", schedule)
	return nil
}

// Start begins firing. The first run of a job is its first scheduled moment,
// never now: a rolling deploy brings every replica up within seconds of each
// other, and jobs that ran on boot would all sweep the same work at once.
func (s *Scheduler) Start(ctx context.Context) error {
	if s.closed {
		// The library cannot restart one that has been shut down, and a
		// scheduler that silently never fires again is worse than a refusal.
		return errors.New("scheduler: already shut down")
	}
	if s.started {
		return errors.New("scheduler: already started")
	}
	s.started = true

	s.running, s.cancel = context.WithCancel(context.WithoutCancel(ctx))
	s.cron.Start()
	return nil
}

// Shutdown stops firing and lets a run in flight finish, within StopTimeout.
//
// It runs even for a scheduler that was never started: one is alive from the
// moment it is built, with goroutines of its own, so skipping this would leak
// them on any startup that failed after this was created.
//
// Safe to call twice, because shutdown paths cross.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.started = false

	// Bounded twice on purpose: StopTimeout is what the library waits, and ctx
	// is the process's whole shutdown budget. Whichever is shorter wins, and
	// neither can be left out -- the library's own wait would not notice the
	// budget, and the budget alone would leave its goroutines running.
	stopped := make(chan error, 1)
	go func() { stopped <- s.cron.Shutdown() }()

	var err error
	select {
	case err = <-stopped:
	case <-ctx.Done():
		s.log.WarnContext(ctx, "scheduled jobs did not finish in time")
	}

	if s.cancel != nil {
		s.cancel()
	}
	if err != nil {
		return fmt.Errorf("scheduler: shutdown: %w", err)
	}
	return nil
}
