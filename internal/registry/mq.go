package registry

import (
	"context"

	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/messagequeue"
)

// NATS opens the connection and puts it under res, so readiness asks JetStream
// and shutdown drains it rather than cutting it off.
//
// Stream and DuplicateWindow are deliberately not passed on: they name this
// service's own stream, and building it belongs to the adapter. This is the
// only place the url is revealed; everywhere else it stays an env.Secret that
// prints itself redacted.
func NATS(
	ctx context.Context,
	s settings.MQ,
	res *Resources,
) (*messagequeue.NATS, error) {
	mq, err := messagequeue.New(messagequeue.Config{
		URL:            s.URL.Reveal(),
		ConnectTimeout: s.ConnectTimeout,
		MaxReconnects:  s.MaxReconnects,
		ReconnectWait:  s.ReconnectWait,
		DrainTimeout:   s.DrainTimeout,
	}, res.log)
	if err != nil {
		return nil, err
	}

	if err := mq.Connect(ctx); err != nil {
		return nil, err
	}

	res.add(step{
		tier:  tierBroker,
		name:  "nats",
		ready: mq.Ping,
		close: mq.Drain,
	})
	return mq, nil
}
