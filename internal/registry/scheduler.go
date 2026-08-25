package registry

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/infra/scheduler"
)

// Job is one thing to run on a schedule.
type Job struct {
	Name string

	// Schedule is a cron spec or an interval descriptor -- "*/5 * * * *",
	// "0 3 * * *", "@every 5m". One parser reads all of them.
	Schedule string

	Run func(context.Context) error
}

// Scheduler registers the jobs, starts firing, and registers its stop at the
// top tier.
//
// The top tier for the same reason the consumer takes it: this is work coming
// IN. It has to stop finding things to do before the pool it reads and the
// broker it writes go away.
//
// Every job is passed here rather than added one at a time, so one binary has
// one scheduler and what it runs is readable in one place.
func Scheduler(
	ctx context.Context,
	name string,
	loc *time.Location,
	stopTimeout time.Duration,
	jobs []Job,
	res *Resources,
) (*scheduler.Scheduler, error) {
	s, err := scheduler.New(scheduler.Config{Location: loc, StopTimeout: stopTimeout}, res.log)
	if err != nil {
		return nil, err
	}

	for _, j := range jobs {
		if err := s.Add(j.Name, j.Schedule, j.Run); err != nil {
			return nil, err
		}
	}

	if err := s.Start(ctx); err != nil {
		return nil, err
	}

	res.add(step{
		tier:  tierServer,
		name:  name,
		close: s.Shutdown,
	})
	return s, nil
}
