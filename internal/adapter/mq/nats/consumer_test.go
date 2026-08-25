//go:build integration

// The retrying, the redelivery count and the ack semantics are the broker's,
// not ours. A fake would only prove that our fake redelivers.
//
// Run the dependencies first: make dev-up
package nats

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/nats-io/nats.go/jetstream"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// handled records what the core was asked to do, and answers however the test
// tells it to.
type handled struct {
	mu       sync.Mutex
	attempts []int
	ids      []shared.ID
	at       []time.Time
	answer   func(attempt int) error

	done chan struct{}
	want int
}

func newHandled(want int, answer func(int) error) *handled {
	return &handled{answer: answer, done: make(chan struct{}), want: want}
}

func (h *handled) Handle(_ context.Context, id shared.ID, attempt int) error {
	h.mu.Lock()
	h.attempts = append(h.attempts, attempt)
	h.ids = append(h.ids, id)
	h.at = append(h.at, time.Now())
	reached := len(h.attempts) == h.want
	h.mu.Unlock()

	if reached {
		close(h.done)
	}
	if h.answer == nil {
		return nil
	}
	return h.answer(attempt)
}

func (h *handled) wait(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(d):
		h.mu.Lock()
		defer h.mu.Unlock()
		t.Fatalf("handled %d of %d in %v: attempts %v", len(h.attempts), h.want, d, h.attempts)
	}
}

func (h *handled) seen() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int(nil), h.attempts...)
}

// consuming publishes nothing; it wires a stream, a consumer and a handler, and
// hands back the publisher so a test can put events on it.
func consuming(
	t *testing.T, name string, maxAttempts int, h *handled,
) (*DispatchPublisher, jetstream.JetStream, Stream) {
	t.Helper()

	js := connect(t)
	stream := freshStream(t, js, name, time.Minute)

	// Deleted with the stream, but named here so a leftover from a failed run
	// cannot be picked up by the next one.
	_ = js.DeleteConsumer(context.Background(), name, dispatchConsumer)

	c, err := NewConsumer(context.Background(), js, ConsumerConfig{
		Stream:      stream,
		MaxAttempts: maxAttempts,
		// Short, so a test that waits for a redelivery does not wait a minute.
		AckWait:     2 * time.Second,
		MaxInFlight: 10,
	}, h, quiet())
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Stop(ctx)
	})

	p, err := NewDispatchPublisher(js, stream)
	if err != nil {
		t.Fatalf("NewDispatchPublisher: %v", err)
	}
	return p, js, stream
}

func TestAPublishedEventReachesTheCore(t *testing.T) {
	h := newHandled(1, nil)
	p, js, stream := consuming(t, "TEST_CONSUME", 5, h)

	e := event("01J8XKQ2R7M3NB4PZC5VD6C701", shared.ChannelEmail, shared.PriorityHigh)
	if err := p.Publish(context.Background(), e); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	h.wait(t, 10*time.Second)

	if got := h.ids[0]; got != e.DeliveryID {
		t.Errorf("delivery id = %q, want %q", got, e.DeliveryID)
	}
	if got := h.seen()[0]; got != 1 {
		t.Errorf("first attempt = %d, want 1", got)
	}

	// Acknowledged, so WorkQueue retention drops it.
	if got := pending(t, js, stream.Name); got != 0 {
		t.Errorf("stream holds %d messages after a handled event, want 0", got)
	}
}

// Returning an error asks for the event again, with two things worth proving.
//
// The count is the broker's own, which is what lets the core know when an
// attempt is the last one. And the second attempt waits: a plain Nak would be
// redelivered at once -- the broker's backoff applies only to messages that
// were never acknowledged and skips one that was nak'ed -- so the delay has to
// travel with the nak itself.
//
// Two attempts prove both. A third would only prove the second interval, at the
// cost of thirty seconds.
func TestAnErrorAsksAgainAfterWaiting(t *testing.T) {
	h := newHandled(2, func(int) error { return errors.New("provider said no") })
	p, _, _ := consuming(t, "TEST_RETRY", 5, h)

	e := event("01J8XKQ2R7M3NB4PZC5VD6C702", shared.ChannelTelegram, shared.PriorityNormal)
	if err := p.Publish(context.Background(), e); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	h.wait(t, 30*time.Second)

	got := h.seen()
	if got[0] != 1 || got[1] != 2 {
		t.Errorf("attempts = %v, want the broker counting 1 then 2", got)
	}

	h.mu.Lock()
	waited := h.at[1].Sub(h.at[0])
	h.mu.Unlock()

	// A shade under, because the two clocks are the same one but the broker
	// rounds.
	if floor := backoff[0] - 500*time.Millisecond; waited < floor {
		t.Errorf("the second attempt came after %v, want at least %v -- a plain "+
			"nak would have been redelivered at once", waited, floor)
	}
}

// The broker stops at the limit, and the limit is the same number the core
// judges a last chance by -- so the attempt it sees as the last really is.
func TestTheBrokerStopsAtTheLimit(t *testing.T) {
	const maxAttempts = 2

	h := newHandled(maxAttempts, func(int) error { return errors.New("still no") })
	p, _, _ := consuming(t, "TEST_LIMIT", maxAttempts, h)

	e := event("01J8XKQ2R7M3NB4PZC5VD6C703", shared.ChannelBale, shared.PriorityNormal)
	if err := p.Publish(context.Background(), e); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	h.wait(t, 40*time.Second)

	// Long enough that a third would have arrived if the limit were not held.
	time.Sleep(8 * time.Second)

	if got := len(h.seen()); got != maxAttempts {
		t.Errorf("the core was asked %d times, want %d", got, maxAttempts)
	}
}

// A message that will not decode will not decode next time either. Asking again
// would occupy the queue forever, so it is terminated.
func TestAnUndecodableMessageIsNotAskedForAgain(t *testing.T) {
	h := newHandled(1, nil)
	_, js, stream := consuming(t, "TEST_GARBAGE", 5, h)

	subject, err := stream.Subjects.ForDispatch(
		event("01J8XKQ2R7M3NB4PZC5VD6C704", shared.ChannelEmail, shared.PriorityNormal),
	)
	if err != nil {
		t.Fatalf("ForDispatch: %v", err)
	}
	if _, err := js.Publish(context.Background(), subject, []byte("not an event")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Long enough for a redelivery to have happened if it were going to.
	time.Sleep(6 * time.Second)

	if got := len(h.seen()); got != 0 {
		t.Errorf("the core was asked %d times about a message it cannot read", got)
	}
	if got := pending(t, js, stream.Name); got != 0 {
		t.Errorf("stream holds %d messages, want the garbage terminated", got)
	}
}

// pending is what the stream still holds. WorkQueue drops a message once it is
// acknowledged or terminated, so this is the count of unfinished work.
func pending(t *testing.T, js jetstream.JetStream, name string) uint64 {
	t.Helper()

	s, err := js.Stream(context.Background(), name)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	info, err := s.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	return info.State.Msgs
}

// Pull, not push. A consumer is push only if DeliverSubject is set, so the
// choice is made by leaving a field out -- and a field nobody set is a field
// somebody can set by accident.
//
// It matters twice over: with pull a burst waits in the stream rather than in
// this process's memory, and several dispatchers can share one durable consumer
// with the broker handing each message to exactly one of them.
func TestTheConsumerIsPullNotPush(t *testing.T) {
	h := newHandled(1, nil)
	_, js, stream := consuming(t, "TEST_PULL", 5, h)

	c, err := js.Consumer(context.Background(), stream.Name, dispatchConsumer)
	if err != nil {
		t.Fatalf("Consumer: %v", err)
	}
	cfg := c.CachedInfo().Config

	if cfg.DeliverSubject != "" {
		t.Errorf("DeliverSubject = %q, which makes this a push consumer", cfg.DeliverSubject)
	}
	if cfg.Durable != dispatchConsumer {
		t.Errorf("Durable = %q, want %q -- without it a restart re-reads the stream",
			cfg.Durable, dispatchConsumer)
	}
	if cfg.AckPolicy != jetstream.AckExplicitPolicy {
		t.Errorf("ack policy = %v, want explicit", cfg.AckPolicy)
	}
	if cfg.MaxDeliver != 5 {
		t.Errorf("MaxDeliver = %d, want the core's own limit", cfg.MaxDeliver)
	}
}
