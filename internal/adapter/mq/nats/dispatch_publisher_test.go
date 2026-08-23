//go:build integration

// These need a real broker. The dedup this file is mostly about is the
// broker's, not ours -- a fake would only prove that our fake deduplicates.
//
// Run the dependencies first: make dev-up
package nats

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// url is where the local broker listens. Overridable so the same tests can be
// pointed at a throwaway one in CI.
func url() string {
	if v := os.Getenv("NOTIF_TEST_MQ_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:7002"
}

// connect skips rather than fails when nothing is listening. Somebody running
// with the tag but without the containers should be told what is missing, not
// handed a wall of dial errors.
func connect(t *testing.T) jetstream.JetStream {
	t.Helper()

	conn, err := natsgo.Connect(url(), natsgo.Timeout(3*time.Second))
	if err != nil {
		t.Skipf("no broker: %v (run: make dev-up)", err)
	}
	t.Cleanup(conn.Close)

	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	return js
}

// freshStream gives each test a stream of its own, deleted afterwards, so one
// test's messages can never be counted by another.
func freshStream(
	t *testing.T, js jetstream.JetStream, name string, dedup time.Duration,
) Stream {
	t.Helper()

	// Its own root as well as its own name, so one test's stream cannot capture
	// what another publishes.
	subjects, err := NewSubjects(strings.ToLower(name))
	if err != nil {
		t.Fatalf("NewSubjects: %v", err)
	}
	stream, err := NewStream(name, subjects)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}

	ctx := context.Background()
	_ = js.DeleteStream(ctx, name)

	err = EnsureStream(ctx, js, StreamConfig{
		Stream:          stream,
		DuplicateWindow: dedup,
		MaxAge:          24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("EnsureStream: %v", err)
	}
	t.Cleanup(func() { _ = js.DeleteStream(context.Background(), name) })
	return stream
}

func stored(t *testing.T, js jetstream.JetStream, name string) uint64 {
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

func event(id string, c shared.Channel, p shared.Priority) shared.DispatchEvent {
	return shared.DispatchEvent{
		DeliveryID: shared.ID(id),
		SourceID:   "01J8XKQ2R7M3NB4PZC5VD6S001",
		Channel:    c,
		Priority:   p,
	}
}

// EnsureStream runs at every startup, so running it twice has to be as
// uneventful as running it once.
func TestEnsureStreamIsSafeToRunAgain(t *testing.T) {
	js := connect(t)
	const name = "TEST_ENSURE"

	stream := freshStream(t, js, name, time.Minute)
	if err := EnsureStream(context.Background(), js, StreamConfig{
		Stream: stream, DuplicateWindow: time.Minute, MaxAge: 24 * time.Hour,
	}); err != nil {
		t.Fatalf("second EnsureStream: %v", err)
	}

	s, err := js.Stream(context.Background(), name)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	info, err := s.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	if info.Config.Retention != jetstream.WorkQueuePolicy {
		t.Errorf("retention = %v, want work queue", info.Config.Retention)
	}
	if info.Config.Storage != jetstream.FileStorage {
		t.Errorf("storage = %v, want file -- a restart would forget every event",
			info.Config.Storage)
	}
	if info.Config.Duplicates != time.Minute {
		t.Errorf("duplicate window = %v, want a minute", info.Config.Duplicates)
	}
	if len(info.Config.Subjects) != 1 || info.Config.Subjects[0] != stream.Subjects.Wildcard() {
		t.Errorf("subjects = %v, want %q", info.Config.Subjects, stream.Subjects.Wildcard())
	}
}

func TestPublishStoresOneMessageOnTheRightSubject(t *testing.T) {
	js := connect(t)
	const name = "TEST_PUBLISH"

	stream := freshStream(t, js, name, time.Minute)

	p, err := NewDispatchPublisher(js, stream)
	if err != nil {
		t.Fatalf("NewDispatchPublisher: %v", err)
	}

	e := event("01J8XKQ2R7M3NB4PZC5VD6E701", shared.ChannelEmail, shared.PriorityHigh)
	if err := p.Publish(context.Background(), e); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if got := stored(t, js, name); got != 1 {
		t.Fatalf("stream holds %d messages, want 1", got)
	}

	s, err := js.Stream(context.Background(), name)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	msg, err := s.GetMsg(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetMsg: %v", err)
	}

	want := stream.Subjects.Root() + ".email.high"
	if msg.Subject != want {
		t.Errorf("subject = %q, want %q", msg.Subject, want)
	}
	if !strings.Contains(string(msg.Data), string(e.DeliveryID)) {
		t.Errorf("payload does not carry the delivery id: %s", msg.Data)
	}
}

// The whole reason the delivery id is the message id: a publish that reached
// the broker but whose acknowledgement was lost is retried, and the customer
// must not be messaged twice for it.
func TestTheSameDeliveryPublishedTwiceIsStoredOnce(t *testing.T) {
	js := connect(t)
	const name = "TEST_DEDUP"

	stream := freshStream(t, js, name, time.Minute)

	p, err := NewDispatchPublisher(js, stream)
	if err != nil {
		t.Fatalf("NewDispatchPublisher: %v", err)
	}

	e := event("01J8XKQ2R7M3NB4PZC5VD6E702", shared.ChannelTelegram, shared.PriorityNormal)
	ctx := context.Background()

	for i := range 3 {
		if err := p.Publish(ctx, e); err != nil {
			t.Fatalf("publish %d: %v", i+1, err)
		}
	}

	if got := stored(t, js, name); got != 1 {
		t.Errorf("stream holds %d messages after three publishes of one delivery, want 1", got)
	}
}

// Dedup is per delivery, not per stream. Two recipients of one message are two
// deliveries and must both be sent.
func TestDifferentDeliveriesAreBothStored(t *testing.T) {
	js := connect(t)
	const name = "TEST_DISTINCT"

	stream := freshStream(t, js, name, time.Minute)

	p, err := NewDispatchPublisher(js, stream)
	if err != nil {
		t.Fatalf("NewDispatchPublisher: %v", err)
	}

	ctx := context.Background()
	events := []shared.DispatchEvent{
		event("01J8XKQ2R7M3NB4PZC5VD6E703", shared.ChannelEmail, shared.PriorityNormal),
		event("01J8XKQ2R7M3NB4PZC5VD6E704", shared.ChannelTelegram, shared.PriorityNormal),
		event("01J8XKQ2R7M3NB4PZC5VD6E705", shared.ChannelBale, shared.PriorityCritical),
	}
	for _, e := range events {
		if err := p.Publish(ctx, e); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	if got := stored(t, js, name); got != uint64(len(events)) {
		t.Errorf("stream holds %d messages, want %d", got, len(events))
	}
}

// An event the domain could not have produced is refused before it reaches the
// broker, because a subject nothing listens on is accepted and read by nobody.
func TestAnEventWithAnUnknownChannelNeverReachesTheBroker(t *testing.T) {
	js := connect(t)
	const name = "TEST_REFUSED"

	stream := freshStream(t, js, name, time.Minute)

	p, err := NewDispatchPublisher(js, stream)
	if err != nil {
		t.Fatalf("NewDispatchPublisher: %v", err)
	}

	e := event("01J8XKQ2R7M3NB4PZC5VD6E706", "carrier-pigeon", shared.PriorityNormal)
	if err := p.Publish(context.Background(), e); err == nil {
		t.Fatal("Publish() = nil, want it refused")
	}
	if got := stored(t, js, name); got != 0 {
		t.Errorf("stream holds %d messages, want none", got)
	}
}

// A publish is addressed to a subject, and the broker decides which stream
// captures it. Naming the stream is what turns that decision into something we
// can be wrong about loudly: a second stream configured over our namespace
// would otherwise swallow these events with every publish acknowledged.
func TestAPublishIntoTheWrongStreamIsRefused(t *testing.T) {
	js := connect(t)
	const name = "TEST_EXPECT"

	stream := freshStream(t, js, name, time.Minute)

	// The right subjects, the wrong stream.
	wrong, err := NewStream("SOMEONE_ELSE", stream.Subjects)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	p, err := NewDispatchPublisher(js, wrong)
	if err != nil {
		t.Fatalf("NewDispatchPublisher: %v", err)
	}

	e := event("01J8XKQ2R7M3NB4PZC5VD6E707", shared.ChannelEmail, shared.PriorityNormal)
	if err := p.Publish(context.Background(), e); err == nil {
		t.Fatal("Publish() = nil, want the broker to refuse it")
	}
	if got := stored(t, js, name); got != 0 {
		t.Errorf("stream holds %d messages, want none", got)
	}
}
