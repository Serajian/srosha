package registry

import (
	"context"

	"github.com/Serajian/srosha/internal/adapter/mq/nats"
	"github.com/Serajian/srosha/internal/config/settings"

	"github.com/nats-io/nats.go/jetstream"
)

// Consumer starts reading dispatch events, and registers its stop at the top
// tier.
//
// The top tier is right even though this is not a socket: it is inbound work.
// It has to stop taking messages before the broker it acknowledges to and the
// pool it writes to go away, exactly as a listener stops accepting first.
//
// The consumer itself is built by the adapter, which knows what an event is.
// This only maps the service's settings onto what that needs, and remembers how
// to stop it.
func Consumer(
	ctx context.Context,
	name string,
	js jetstream.JetStream,
	stream nats.Stream,
	d settings.Dispatch,
	handler nats.Dispatcher,
	res *Resources,
) (*nats.Consumer, error) {
	consumer, err := nats.NewConsumer(ctx, js, nats.ConsumerConfig{
		Stream:      stream,
		MaxAttempts: d.MaxAttempts,
		AckWait:     d.AckWait,
		MaxInFlight: d.MaxInFlight,
	}, handler, res.log)
	if err != nil {
		return nil, err
	}

	if err := consumer.Start(ctx); err != nil {
		return nil, err
	}

	res.add(step{
		tier:  tierServer,
		name:  name,
		close: consumer.Stop,
	})
	return consumer, nil
}
