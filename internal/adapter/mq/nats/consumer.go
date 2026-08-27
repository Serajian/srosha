package nats

import (
	"context"
	"log/slog"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/nats-io/nats.go/jetstream"
)

// Dispatcher is what the consumer hands each event to.
//
// Declared here rather than imported, because one method is all this needs and
// an interface belongs to whoever calls it. usecase.Dispatcher satisfies it.
//
// Returning nil means the broker is done with the event, whether the message
// went out or failed for good. Returning an error asks for it again.
type Dispatcher interface {
	Handle(ctx context.Context, id shared.ID, attempt int) error
}

// ConsumerConfig is what this consumer needs to exist on the broker.
type ConsumerConfig struct {
	Stream Stream

	// MaxAttempts is the broker's delivery limit AND the core's, deliberately
	// the same number. The core decides a delivery has had its last chance when
	// the broker's own count reaches it, so a broker that gave up earlier would
	// leave the row pending with no outcome ever written on it, and one that
	// gave up later would keep offering a delivery already marked failed.
	MaxAttempts int

	// AckWait is how long the broker waits before deciding nobody handled the
	// message. It also bounds the work: past it the message has been given to
	// somebody else, so carrying on would be a second send of the same thing.
	AckWait time.Duration

	// MaxInFlight is how many deliveries are worked on at once.
	MaxInFlight int
}

// Consumer reads dispatch events and hands them to the core.
//
// It is a driving adapter -- it calls in rather than being called -- so it
// implements no port, exactly as the gRPC server implements none. There is
// nothing to invert: the broker is what starts the work.
type Consumer struct {
	consumer jetstream.Consumer
	handler  Dispatcher
	log      *slog.Logger

	ackWait time.Duration
	backoff []time.Duration

	// running is the context every handled message inherits, canceled by Stop
	// so work in flight is told to give up rather than finishing into a broker
	// that is already gone.
	running context.Context
	stop    context.CancelFunc

	consuming jetstream.ConsumeContext
}

// NewConsumer creates the consumer on the broker, or updates it to match. It
// binds nothing and reads nothing until Start.
//
// Durable, so a restart carries on where it left off rather than re-reading the
// stream from the beginning.
func NewConsumer(
	ctx context.Context,
	js jetstream.JetStream,
	cfg ConsumerConfig,
	handler Dispatcher,
	log *slog.Logger,
) (*Consumer, error) {
	switch {
	case js == nil:
		return nil, errs.InternalErr("no jetstream handle")
	case handler == nil:
		return nil, errs.InternalErr("consumer has nothing to hand events to")
	case log == nil:
		return nil, errs.InternalErr("consumer has no logger")
	case cfg.Stream.IsZero():
		return nil, errs.InternalErr("consumer has no stream")
	case cfg.MaxAttempts <= 0:
		return nil, errs.InternalErr("consumer needs a delivery limit above zero")
	case cfg.AckWait <= 0:
		return nil, errs.InternalErr("consumer needs an ack wait above zero")
	case cfg.MaxInFlight <= 0:
		return nil, errs.InternalErr("consumer needs a limit on messages in flight")
	}

	delays := delaysFor(cfg.MaxAttempts)

	// PULL, and it is a decision rather than a default -- a consumer is push
	// only if DeliverSubject is set, so the choice here is made by leaving a
	// field out, which is a poor way to say something this load-bearing.
	//
	// Pull, for two reasons that matter to this service:
	//
	//   Flow control is ours. The broker hands over work when we ask for it, so
	//   a burst sits in the stream rather than in this process's memory. A push
	//   consumer sends as fast as it likes and we buffer whatever arrives.
	//
	//   Several dispatchers share one durable consumer. The broker gives each
	//   message to exactly one of them, so scaling out is another container and
	//   no new configuration -- which is what WorkQueue retention wants anyway,
	//   since it allows only one consumer over a subject.
	//
	// Consume reads like push because it takes a callback, but it is continuous
	// polling underneath: the option type is PullConsumeOpt.
	consumer, err := js.CreateOrUpdateConsumer(ctx, cfg.Stream.Name, jetstream.ConsumerConfig{
		Durable:       dispatchConsumer,
		FilterSubject: cfg.Stream.Subjects.Wildcard(),

		// Explicit, because every other policy acknowledges messages this one
		// has not finished with: a delivery is done when the row says so, not
		// when the message arrived.
		AckPolicy: jetstream.AckExplicitPolicy,

		MaxDeliver:    cfg.MaxAttempts,
		AckWait:       cfg.AckWait,
		BackOff:       delays,
		MaxAckPending: cfg.MaxInFlight,
	})
	if err != nil {
		return nil, errs.UnavailableErr("the request could not be completed").
			WithStr("create consumer on " + cfg.Stream.Name).
			WithErr(err)
	}

	running, stop := context.WithCancel(context.WithoutCancel(ctx))

	return &Consumer{
		consumer: consumer,
		handler:  handler,
		log:      log,
		ackWait:  cfg.AckWait,
		backoff:  delays,
		running:  running,
		stop:     stop,
	}, nil
}

// Start begins consuming. It returns as soon as the subscription is in place;
// the messages arrive on the broker's own goroutines afterwards.
func (c *Consumer) Start(ctx context.Context) error {
	if c.consuming != nil {
		return errs.InternalErr("consumer already started")
	}

	consuming, err := c.consumer.Consume(c.handle)
	if err != nil {
		return errs.UnavailableErr("the request could not be completed").
			WithStr("start consuming").
			WithErr(err)
	}
	c.consuming = consuming

	c.log.InfoContext(ctx, "consuming", "consumer", dispatchConsumer)
	return nil
}

// Stop drains what is in flight and then stops.
//
// Drain rather than Stop on the subscription: a message already being handled
// gets to finish and acknowledge, which is the difference between a redelivery
// after a restart and none. The context bounds that wait, and canceling
// c.running tells the work itself to stop pretending it has time.
//
// Safe to call twice, because shutdown paths cross.
func (c *Consumer) Stop(ctx context.Context) error {
	if c.consuming == nil {
		return nil
	}
	consuming := c.consuming
	c.consuming = nil

	drained := make(chan struct{})
	go func() {
		consuming.Drain()
		<-consuming.Closed()
		close(drained)
	}()

	select {
	case <-drained:
	case <-ctx.Done():
		c.log.WarnContext(ctx, "consumer did not drain in time")
		consuming.Stop()
	}

	c.stop()
	return nil
}

// handle is what the broker calls, once per delivered message.
//
// Every path ends in exactly one of ack, nak or term, because a message that
// gets none of the three sits until AckWait expires and is delivered again --
// which looks like a message nobody could handle rather than one nobody
// answered.
func (c *Consumer) handle(msg jetstream.Msg) {
	meta, err := msg.Metadata()
	if err != nil {
		// Not a jetstream message at all. Nothing about a retry would change
		// that, so it is terminated rather than left to circle.
		c.log.ErrorContext(c.running, "message has no metadata", "err", err)
		c.term(msg)
		return
	}

	event, err := decode(msg.Data())
	if err != nil {
		// It will not decode next time either: it was written by a version
		// whose shape we no longer read, or it is not ours.
		c.log.ErrorContext(c.running, "event could not be decoded",
			"subject", msg.Subject(), "seq", meta.Sequence.Stream, "err", err)
		c.term(msg)
		return
	}

	// The broker's own count, bounded by MaxDeliver. On its own line so that
	// golines cannot wrap the statement out from under it.
	//nolint:gosec // see above
	attempt := int(meta.NumDelivered)

	// Bounded by AckWait: past that the broker has already offered this message
	// to somebody else, and finishing would be a second send of one delivery.
	ctx, cancel := context.WithTimeout(c.running, c.ackWait)
	defer cancel()

	if err := c.handler.Handle(ctx, event.DeliveryID, attempt); err != nil {
		c.log.WarnContext(ctx, "delivery not settled, asking again",
			"delivery_id", event.DeliveryID, "attempt", attempt, "err", err)
		c.nak(msg, attempt)
		return
	}

	if err := msg.Ack(); err != nil {
		// The work is done and the row says so. A lost ack costs one redelivery,
		// which IsSettled turns into nothing.
		c.log.WarnContext(ctx, "could not acknowledge",
			"delivery_id", event.DeliveryID, "err", err)
	}
}

// nak asks for the message again, carrying the delay itself: the broker's own
// backoff applies only to messages that were never acknowledged, and skips one
// that was explicitly nak'ed. Without the delay a failing provider would be
// hammered as fast as the loop can turn.
func (c *Consumer) nak(msg jetstream.Msg, attempt int) {
	if err := msg.NakWithDelay(c.delay(attempt)); err != nil {
		c.log.WarnContext(c.running, "could not nak", "err", err)
	}
}

func (c *Consumer) term(msg jetstream.Msg) {
	if err := msg.Term(); err != nil {
		c.log.WarnContext(c.running, "could not terminate", "err", err)
	}
}

// delay is how long before this message is offered again. attempt is the
// broker's count and starts at one, so the first failure waits backoff[0].
func (c *Consumer) delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(c.backoff) {
		attempt = len(c.backoff)
	}
	return c.backoff[attempt-1]
}

// delaysFor trims the table to the delivery limit. The broker refuses a
// consumer with more intervals than attempts, and the last interval repeats for
// whatever is left, so trimming loses nothing.
func delaysFor(maxAttempts int) []time.Duration {
	if maxAttempts >= len(backoff) {
		return backoff
	}
	return backoff[:maxAttempts]
}
