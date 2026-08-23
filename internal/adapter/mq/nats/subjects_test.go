package nats

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/shared"
)

const deliveryID = shared.ID("01J8XKQ2R7M3NB4PZC5VD6E701")

func dispatchSubjects(t *testing.T) Subjects {
	t.Helper()

	s, err := DispatchStream("NOTIFY")
	if err != nil {
		t.Fatalf("DispatchStream: %v", err)
	}
	return s.Subjects
}

func anEvent(c shared.Channel, p shared.Priority) shared.DispatchEvent {
	return shared.DispatchEvent{
		DeliveryID: deliveryID,
		SourceID:   "01J8XKQ2R7M3NB4PZC5VD6S001",
		Channel:    c,
		Priority:   p,
	}
}

func TestForDispatch(t *testing.T) {
	subjects := dispatchSubjects(t)

	tests := []struct {
		channel  shared.Channel
		priority shared.Priority
		want     string
	}{
		{shared.ChannelEmail, shared.PriorityNormal, "notify.email.normal"},
		{shared.ChannelTelegram, shared.PriorityHigh, "notify.telegram.high"},
		{shared.ChannelBale, shared.PriorityCritical, "notify.bale.critical"},
		{shared.ChannelWhatsApp, shared.PriorityNormal, "notify.whatsapp.normal"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := subjects.ForDispatch(anEvent(tt.channel, tt.priority))
			if err != nil {
				t.Fatalf("ForDispatch() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ForDispatch() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every subject a real event produces has to be captured by the stream, or the
// broker accepts the publish and nobody ever reads it.
func TestEverySubjectFallsUnderTheStreamsWildcard(t *testing.T) {
	subjects := dispatchSubjects(t)
	prefix := subjects.Root() + "."

	for _, c := range shared.AllChannels() {
		for _, p := range []shared.Priority{
			shared.PriorityNormal, shared.PriorityHigh, shared.PriorityCritical,
		} {
			got, err := subjects.ForDispatch(anEvent(c, p))
			if err != nil {
				t.Fatalf("ForDispatch(%v, %v) error = %v", c, p, err)
			}
			if !strings.HasPrefix(got, prefix) || len(got) == len(prefix) {
				t.Errorf("ForDispatch() = %q, outside %q", got, subjects.Wildcard())
			}
		}
	}
}

// A subject built from a value the domain does not know would be published to
// something nothing listens on: accepted by the broker, read by nobody. That is
// worse than an error, so it is one.
func TestForDispatchRefusesWhatNothingWouldListenTo(t *testing.T) {
	subjects := dispatchSubjects(t)

	tests := []struct {
		name  string
		event shared.DispatchEvent
	}{
		{"unknown channel", anEvent("carrier-pigeon", shared.PriorityNormal)},
		{"empty channel", anEvent("", shared.PriorityNormal)},
		{"unknown priority", anEvent(shared.ChannelEmail, shared.Priority(42))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := subjects.ForDispatch(tt.event)
			if err == nil {
				t.Fatalf("ForDispatch() = %q, want an error", got)
			}
		})
	}
}

// The two binaries are deployed separately, so a message one version publishes
// is read by another. These field names are that contract; changing them is a
// deployment with a gap in it.
func TestTheWireFormatIsWhatTheOtherBinaryExpects(t *testing.T) {
	e := anEvent(shared.ChannelEmail, shared.PriorityHigh)
	data, err := encode(e, e.DeliveryID.String())
	if err != nil {
		t.Fatalf("encode() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("what we published is not json: %v", err)
	}

	want := map[string]any{
		"delivery_id": string(deliveryID),
		"source_id":   "01J8XKQ2R7M3NB4PZC5VD6S001",
		"channel":     "email",
		// A name, not the iota. Reordering the constants can never make an
		// already-published message mean a different priority.
		"priority": "HIGH",
	}
	if len(got) != len(want) {
		t.Fatalf("the event carries %d fields, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

// The event sits in the broker's file storage and in its logs, so it carries
// identifiers and nothing a person could be found by.
func TestTheEventCarriesNothingPersonal(t *testing.T) {
	e := anEvent(shared.ChannelEmail, shared.PriorityNormal)
	data, err := encode(e, e.DeliveryID.String())
	if err != nil {
		t.Fatalf("encode() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, forbidden := range []string{"address", "title", "body", "recipient", "email", "phone"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("the event gained a %q field", forbidden)
		}
	}
}

// A second stream is a second root. Nothing about building its subjects is
// written on the assumption that there is only one.
func TestASecondStreamGetsItsOwnNamespace(t *testing.T) {
	dispatch := dispatchSubjects(t)

	audit, err := NewSubjects("audit")
	if err != nil {
		t.Fatalf("NewSubjects: %v", err)
	}

	if audit.Wildcard() != "audit.>" {
		t.Errorf("Wildcard() = %q, want audit.>", audit.Wildcard())
	}
	if audit.Wildcard() == dispatch.Wildcard() {
		t.Error("two roots produced one wildcard, so each stream would capture the other's")
	}

	got, err := audit.ForDispatch(anEvent(shared.ChannelEmail, shared.PriorityNormal))
	if err != nil {
		t.Fatalf("ForDispatch: %v", err)
	}
	if got != "audit.email.normal" {
		t.Errorf("ForDispatch() = %q, want audit.email.normal", got)
	}
}

// A root that is not a single token does not fail loudly at publish time. It
// quietly changes what the stream captures, which is why it is refused here.
func TestNewSubjectsRefusesARootThatIsNotOneToken(t *testing.T) {
	tests := []struct {
		name string
		root string
	}{
		{"empty", ""},
		{"two tokens", "notify.v1"},
		{"trailing dot", "notify."},
		{"tail wildcard", "notify>"},
		{"token wildcard", "*"},
		{"a space", "notify events"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSubjects(tt.root)
			if err == nil {
				t.Fatalf("NewSubjects(%q) = %q, want it refused", tt.root, got.Wildcard())
			}
			if !got.IsZero() {
				t.Error("a refused root still produced usable subjects")
			}
		})
	}
}

// The zero value would build ".>", which captures every subject on the broker
// -- including other services'. Nothing accepts it.
func TestTheZeroValueIsRefusedEverywhere(t *testing.T) {
	var zero Subjects

	if !zero.IsZero() {
		t.Fatal("IsZero() = false on the zero value")
	}
	if _, err := zero.ForDispatch(anEvent(shared.ChannelEmail, shared.PriorityNormal)); err == nil {
		t.Error("ForDispatch accepted subjects nobody built")
	}
	if _, err := NewStream("NOTIFY", zero); err == nil {
		t.Error("NewStream accepted subjects nobody built")
	}
	if _, err := NewDispatchPublisher(nil, Stream{Name: "NOTIFY"}); err == nil {
		t.Error("NewDispatchPublisher accepted a stream with no subjects")
	}
}

// The stream's name is configured because operators name their brokers; the
// namespace is not, because it is the protocol both binaries have to agree on
// without being told.
func TestDispatchStreamPairsTheConfiguredNameWithOurOwnNamespace(t *testing.T) {
	got, err := DispatchStream("NOTIFY")
	if err != nil {
		t.Fatalf("DispatchStream: %v", err)
	}

	if got.Name != "NOTIFY" {
		t.Errorf("Name = %q, want the configured one", got.Name)
	}
	if got.Subjects.Root() != DispatchRoot {
		t.Errorf("Root() = %q, want %q", got.Subjects.Root(), DispatchRoot)
	}
	if got.IsZero() {
		t.Error("IsZero() = true on a stream that was built")
	}

	if _, err := DispatchStream(""); err == nil {
		t.Error("DispatchStream accepted a stream with no name")
	}
}
