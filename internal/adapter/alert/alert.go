// Package alert carries what an operator should know to a channel that is not
// this service's own.
//
// Not through srosha's pipeline, deliberately. An alert that traveled the path
// it reports on would be silent exactly when it matters: "postgres is down"
// cannot be delivered by something that needs postgres. So this holds its own
// way out and shares nothing with a customer's message.
package alert

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Pusher is the one thing this package needs from a channel.
//
// Declared here rather than imported: one adapter may not import another, and
// bootstrap is the one place that sees both. It passes a gotify.Sender in.
type Pusher interface {
	Send(ctx context.Context, m shared.Message) (string, error)
}

// Config is what this package needs. Both are operational, so both come from
// configuration rather than from a constant here.
type Config struct {
	// Queue is how many alerts may wait before one is dropped.
	Queue int

	// Timeout bounds one push. Nothing waits on it; this only decides how long
	// a dead server ties up the single worker.
	Timeout time.Duration
}

// Alerter sends alerts, or drops them. It never fails a caller.
type Alerter struct {
	pusher  Pusher
	address string
	timeout time.Duration
	log     *slog.Logger

	queue chan item
	done  sync.WaitGroup

	// stopped is closed by Close, and the queue never is.
	//
	// Closing the queue would be the obvious way to stop the worker, and it is
	// wrong: a Notify racing a Close would send on a closed channel and panic
	// the process -- from the one component whose whole job is to never be
	// able to do that. A separate signal costs one channel and removes the
	// race entirely.
	stopped chan struct{}
	closing sync.Once
}

type item struct{ subject, detail string }

// New builds an alerter, or a silent one.
//
// A nil pusher is not an error: on a laptop nobody has a Gotify, and the
// feature must cost nothing there -- no queue, no goroutine.
func New(p Pusher, address string, cfg Config, log *slog.Logger) *Alerter {
	a := &Alerter{pusher: p, address: address, timeout: cfg.Timeout, log: log}
	if p == nil {
		return a
	}

	a.queue = make(chan item, cfg.Queue)
	a.stopped = make(chan struct{})
	a.done.Add(1)
	go a.run()
	return a
}

// Notify queues an alert, or drops it.
//
// Dropping is the correct answer and not a compromise. Whatever called this is
// in the middle of doing something real -- registering a source, starting a
// process -- and an alert that made that wait would be worse than no alert at
// all. The queue is the entire budget.
func (a *Alerter) Notify(ctx context.Context, subject, detail string) {
	if a.queue == nil {
		return
	}

	select {
	case a.queue <- item{subject: subject, detail: detail}:
	default:
		a.log.WarnContext(ctx, "alert dropped: the queue is full", "subject", subject)
	}
}

// Close stops the worker once it has sent what is already queued.
func (a *Alerter) Close(context.Context) error {
	if a.queue == nil {
		return nil
	}

	a.closing.Do(func() { close(a.stopped) })
	a.done.Wait()
	return nil
}

// run is the only thing that talks to the pusher, so one slow server delays
// alerts and nothing else.
func (a *Alerter) run() {
	defer a.done.Done()

	for {
		select {
		case it := <-a.queue:
			a.push(it)
		case <-a.stopped:
			a.drain()
			return
		}
	}
}

// drain sends what was already queued when Close was called, and stops at the
// first thing that is not there. Anything a Notify adds after this is dropped
// on the floor, which is correct: the process is going away.
func (a *Alerter) drain() {
	for {
		select {
		case it := <-a.queue:
			a.push(it)
		default:
			return
		}
	}
}

func (a *Alerter) push(it item) {
	// Its own context: the caller's is long gone, and an alert must not be
	// canceled because the request that caused it finished.
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()

	_, err := a.pusher.Send(ctx, message(a.address, it))
	if err == nil {
		return
	}

	// Logged and forgotten. A retried alert arrives after the operator has
	// found out some other way, and a queue that drains slowly becomes a queue
	// that reports yesterday.
	a.log.WarnContext(ctx, "alert not delivered", "subject", it.subject, "error", err)
}
