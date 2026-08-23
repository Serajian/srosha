package nats

import (
	"context"
	"time"

	"github.com/Serajian/srosha/pkg/errs"

	"github.com/nats-io/nats.go/jetstream"
)

// Stream is one stream's identity: what it is called, and what it captures.
//
// The two are one value because they travel together everywhere and are useless
// apart. A name says nothing about what lands in the stream, and a namespace
// says nothing about which stream holds it -- and code that took them
// separately could be handed one stream's name with another's subjects.
type Stream struct {
	Name     string
	Subjects Subjects
}

// DispatchStream is the stream the gateway publishes deliveries to. The name is
// configured because operators name their brokers; the namespace is not,
// because it is this service's own protocol and both binaries have to agree on
// it without being told.
//
// This is the one place DispatchRoot is written. A second stream gets its own
// constructor beside this one.
func DispatchStream(name string) (Stream, error) {
	subjects, err := NewSubjects(DispatchRoot)
	if err != nil {
		return Stream{}, err
	}
	return NewStream(name, subjects)
}

func NewStream(name string, subjects Subjects) (Stream, error) {
	if name == "" {
		return Stream{}, errs.InternalErr("stream has no name")
	}
	if subjects.IsZero() {
		return Stream{}, errs.InternalErr("stream has no subjects").WithStr(name)
	}
	return Stream{Name: name, Subjects: subjects}, nil
}

// IsZero reports a Stream nobody built. Its subjects would be ".>", which
// captures every subject on the broker.
func (s Stream) IsZero() bool { return s.Name == "" || s.Subjects.IsZero() }

// StreamConfig is what this service needs the stream to be. The rest of the
// broker's settings are the broker's.
type StreamConfig struct {
	Stream

	// DuplicateWindow is how long the broker remembers a message id. Inside it,
	// publishing the same delivery twice stores one message -- which is the
	// only reason a publish is safe to retry at all.
	DuplicateWindow time.Duration

	// MaxAge stops a stream nobody is draining from growing without bound. It
	// is not what keeps a delayed delivery correct: recovery reads rows, not
	// events, and Handle refuses a delivery that has already settled.
	MaxAge time.Duration
}

// EnsureStream makes the stream match what this service needs, and does it the
// same way whether or not the stream is already there.
//
// Both binaries call it at startup. The gateway cannot publish into a stream
// that does not exist, and waiting for the dispatcher to create one first would
// make the order the two containers happen to start in a thing that matters.
func EnsureStream(ctx context.Context, js jetstream.JetStream, cfg StreamConfig) error {
	if js == nil {
		return errs.InternalErr("no jetstream handle")
	}
	if cfg.IsZero() {
		return errs.InternalErr("stream was never built")
	}

	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     cfg.Name,
		Subjects: []string{cfg.Subjects.Wildcard()},

		// WorkQueue: a message is deleted once it has been acknowledged, and
		// the broker allows only one consumer over a subject. That is exactly
		// what this is -- one delivery, one worker -- and this way the broker
		// enforces it rather than us hoping for it.
		//
		// The cost is that a second consumer, an audit reader say, cannot be
		// added without moving to Interest retention. Nothing needs one: the
		// deliveries table is the record, and the stream is only the hand-off.
		Retention: jetstream.WorkQueuePolicy,

		// File, not memory. The event exists because the rows are already
		// written and somebody has to be told; a broker restart that forgot
		// them would leave every one of those rows for recovery to find.
		Storage: jetstream.FileStorage,

		Duplicates: cfg.DuplicateWindow,
		MaxAge:     cfg.MaxAge,
	})
	if err != nil {
		return errs.UnavailableErr("the request could not be completed").
			WithStr("ensure stream " + cfg.Name).
			WithErr(err)
	}
	return nil
}
