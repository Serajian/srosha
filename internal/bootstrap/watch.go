package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/Serajian/srosha/internal/registry"
)

// teller is what a watcher tells.
//
// Named for what it does rather than what it is, because internal/adapter/
// notifier is a different thing that the dispatcher imports, and one of them
// had to give way.
type teller interface {
	Notify(ctx context.Context, subject, detail string)
}

// watcher turns readiness into news.
//
// Readiness is otherwise only ever asked -- /readyz answers when somebody
// requests it, and nothing inside the process ever does. So a binary knows the
// moment a dependency falls over and tells nobody. That is how three services
// spent a day reporting healthy on a database with no tables.
type watcher struct {
	notify teller
	log    *slog.Logger

	// down is what was wrong last time, by dependency. Absent means it was
	// fine; absent from the whole map means it has never been looked at.
	down map[string]bool
	seen bool
}

func newWatcher(n teller, log *slog.Logger) *watcher {
	return &watcher{notify: n, log: log, down: map[string]bool{}}
}

// run asks on an interval until the context ends.
func (w *watcher) run(ctx context.Context, res *registry.Resources, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.compare(ctx, res.Ready(ctx))
		}
	}
}

// compare reports what changed since the last look, and nothing else.
//
// The first look is deliberately silent. A binary that starts with a dependency
// already down has said so in its startup alert, and saying it again here would
// mean two messages for every restart.
func (w *watcher) compare(ctx context.Context, checks []registry.Check) {
	for _, c := range checks {
		was, known := w.down[c.Name]
		is := c.Err != nil
		w.down[c.Name] = is

		if !w.seen || !known || was == is {
			continue
		}

		if is {
			w.notify.Notify(ctx, c.Name+" is down", c.Err.Error())
			w.log.WarnContext(ctx, "dependency went down", "name", c.Name, "error", c.Err)
			continue
		}
		w.notify.Notify(ctx, c.Name+" is back", "it answers again")
		w.log.InfoContext(ctx, "dependency recovered", "name", c.Name)
	}
	w.seen = true
}
