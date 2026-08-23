package nats

import (
	"context"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/nats-io/nats.go/jetstream"
)

// The core defines what a publisher is; this is the one that speaks to nats. A
// signature that drifts stops the gateway compiling rather than failing at boot.
var _ delivery.Publisher = (*DispatchPublisher)(nil)

// DispatchPublisher publishes dispatch events, and only those. What is specific
// to them lives here -- which subject, and what makes two publishes the same
// one -- and everything else is send below, which a publisher of any other
// event calls unchanged.
//
// It does not create the stream. EnsureStream does, and both binaries call it
// at startup -- a publisher that quietly reconfigured the broker on every
// construction would be a surprising thing to hand to a use case.
type DispatchPublisher struct {
	js     jetstream.JetStream
	stream Stream
}

// NewDispatchPublisher takes the stream rather than building it, so the
// publisher and whoever created that stream are given the same name and the
// same namespace and cannot drift.
func NewDispatchPublisher(js jetstream.JetStream, stream Stream) (*DispatchPublisher, error) {
	if js == nil {
		return nil, errs.InternalErr("no jetstream handle")
	}
	if stream.IsZero() {
		return nil, errs.InternalErr("publisher has no stream")
	}
	return &DispatchPublisher{js: js, stream: stream}, nil
}

// Publish announces one delivery.
//
// The delivery's own id is the message id, which is what makes a publish safe
// to retry: a publish that reached the broker but whose acknowledgement was
// lost can be sent again without the delivery going out twice.
//
//	publish  →  stored  →  ack lost  →  publish again
//	                                    broker knows the id, keeps one message
//
// That works only inside the stream's duplicate window, and only because
// nothing else ever republishes a delivery. Recovery sends rather than
// publishing -- see docs/ARCHITECTURE.md -- and the day that changes, this id
// stops being safe: a deliberate republish would be dropped in silence and read
// as a success.
func (p *DispatchPublisher) Publish(ctx context.Context, e shared.DispatchEvent) error {
	subject, err := p.stream.Subjects.ForDispatch(e)
	if err != nil {
		return err
	}

	data, err := encode(e, e.DeliveryID.String())
	if err != nil {
		return err
	}

	return send(ctx, p.js, p.stream, subject, e.DeliveryID.String(), data)
}

// send is the half that has nothing to do with what is being published.
//
// The stream is named as well as the subject, even though a publish is
// addressed to a subject and the broker decides which stream captures it. That
// decision is the danger: a second stream configured over our namespace would
// swallow these events, and nothing would look wrong -- the publish would be
// acknowledged and no consumer of ours would ever see the message. Naming the
// stream turns that into a refusal.
//
// It waits for the acknowledgement rather than firing and forgetting. A failed
// publish is not fatal by design -- the pending row is the record that this
// must be sent, and recovery finds it -- but that only holds if the failure is
// actually reported. A publish that returned nil and lost the message would
// leave the row to sit until recovery noticed, minutes later.
func send(
	ctx context.Context,
	js jetstream.JetStream,
	stream Stream,
	subject, msgID string,
	data []byte,
) error {
	if _, err := js.Publish(ctx, subject, data,
		jetstream.WithMsgID(msgID),
		jetstream.WithExpectStream(stream.Name),
	); err != nil {
		return errs.UnavailableErr("the request could not be completed").
			WithStr("publish " + subject + " to " + stream.Name).
			WithErr(err)
	}
	return nil
}
