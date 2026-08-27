// These tests are in the package rather than beside it because translation and
// the context key are deliberately not part of the public surface: a source
// that could be put into a context from outside would be an identity check
// anything could bypass.
package grpcsrv

import (
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
	pb "github.com/Serajian/srosha/sdk/go/notification/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// proto3 cannot tell an absent field from one set to its zero value, so the
// zero has to mean "the caller did not say" rather than a real priority.
func TestUnspecifiedPriorityIsNormalAndUnspecifiedChannelIsRefused(t *testing.T) {
	got, err := toPriority(pb.Priority_PRIORITY_UNSPECIFIED)
	if err != nil {
		t.Fatalf("toPriority() error = %v", err)
	}
	if got != shared.PriorityNormal {
		t.Errorf("priority = %v, want normal", got)
	}

	// A channel has no sensible default: sending somewhere the caller did not
	// ask for is worse than telling them they forgot to choose.
	_, err = toChannel(pb.Channel_CHANNEL_UNSPECIFIED)
	if err == nil {
		t.Fatal("an unspecified channel was accepted")
	}
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("type = %v, want invalid input", errs.TypeOf(err))
	}
}

func TestEveryChannelAndPriorityRoundTrips(t *testing.T) {
	for _, c := range shared.AllChannels() {
		if got, err := toChannel(fromChannel(c)); err != nil || got != c {
			t.Errorf("channel %q came back as %q (%v)", c, got, err)
		}
	}
	for _, p := range []shared.Priority{
		shared.PriorityNormal, shared.PriorityHigh, shared.PriorityCritical,
	} {
		if got, err := toPriority(fromPriority(p)); err != nil || got != p {
			t.Errorf("priority %v came back as %v (%v)", p, got, err)
		}
	}
}

// The domain reads a nil expiry as "never". The zero time would read as 1970
// and refuse every message that did not set one.
func TestAnAbsentTimestampStaysAbsentInBothDirections(t *testing.T) {
	if got := toTime(nil); got != nil {
		t.Errorf("toTime(nil) = %v, want nil", got)
	}
	if got := fromTime(nil); got != nil {
		t.Errorf("fromTime(nil) = %v, want nil", got)
	}

	at := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	if got := toTime(timestamppb.New(at)); got == nil || !got.Equal(at) {
		t.Errorf("toTime() = %v, want %v", got, at)
	}
}

// The caller is the source. Read from the body it would be whatever they typed,
// and one customer could send as another -- so it is an argument, and the
// request has nowhere to put one.
func TestTheSourceComesFromTheCallerNotTheRequest(t *testing.T) {
	req := &pb.SubmitRequest{
		Title: "Payment received",
		Body:  "…",
		Routes: []*pb.Route{
			{Channel: pb.Channel_CHANNEL_EMAIL, Address: "a@acme.test", Sender: "billing"},
		},
	}

	cmd, err := toSubmitCommand("01J8XKQ2R7M3NB4PZC5VD6S001", req)
	if err != nil {
		t.Fatalf("toSubmitCommand() error = %v", err)
	}

	if cmd.SourceID != "01J8XKQ2R7M3NB4PZC5VD6S001" {
		t.Errorf("SourceID = %q, want the authenticated one", cmd.SourceID)
	}
	if len(cmd.Routes) != 1 || cmd.Routes[0].Channel != shared.ChannelEmail {
		t.Errorf("routes did not survive: %+v", cmd.Routes)
	}
	if cmd.Senders[shared.ChannelEmail] != "billing" {
		t.Errorf("senders = %v, want billing on email", cmd.Senders)
	}
	if cmd.Priority != shared.PriorityNormal {
		t.Errorf("priority = %v, want the unspecified default", cmd.Priority)
	}
}

// One message sends once per channel, so two routes on one channel naming
// different identities is a request that contradicts itself. Silently letting
// the last one win would send from an identity the caller also asked against.
func TestTwoSendersForOneChannelIsRefused(t *testing.T) {
	req := &pb.SubmitRequest{
		Routes: []*pb.Route{
			{Channel: pb.Channel_CHANNEL_EMAIL, Address: "a@acme.test", Sender: "billing"},
			{Channel: pb.Channel_CHANNEL_EMAIL, Address: "b@acme.test", Sender: "marketing"},
		},
	}

	if _, err := toSubmitCommand("acme", req); err == nil {
		t.Fatal("two senders on one channel were accepted")
	}

	// The same name twice is not a contradiction.
	req.Routes[1].Sender = "billing"
	if _, err := toSubmitCommand("acme", req); err != nil {
		t.Errorf("the same sender twice was refused: %v", err)
	}
}

// Both zero values mean something and neither is an error: no token is the
// first page, no limit is the default.
func TestAnEmptyPageIsTheFirstPage(t *testing.T) {
	got, err := toCursor(nil)
	if err != nil {
		t.Fatalf("toCursor(nil) error = %v", err)
	}
	if got.After != nil || got.Limit != 0 {
		t.Errorf("cursor = %+v, want the zero value", got)
	}

	got, err = toCursor(&pb.Page{Limit: 25})
	if err != nil {
		t.Fatalf("toCursor() error = %v", err)
	}
	if got.After != nil || got.Limit != 25 {
		t.Errorf("cursor = %+v", got)
	}
}

// A page token is an id, and an id from a stranger is a claim like any other.
func TestAMalformedPageTokenIsRefused(t *testing.T) {
	_, err := toCursor(&pb.Page{After: "not-an-id"})
	if err == nil {
		t.Fatal("a malformed token was accepted")
	}
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("type = %v, want invalid input", errs.TypeOf(err))
	}
	if !errors.Is(err, shared.ErrInvalidID) {
		t.Errorf("errors.Is(ErrInvalidID) = false, got %v", err)
	}
}

// A handler with no source in its context has been reached without the auth
// interceptor in front of it. That is our wiring being wrong, not the caller's
// request, and it must not read as a nil source.
func TestSourceFromReportsAnEmptyContext(t *testing.T) {
	if _, ok := SourceFrom(t.Context()); ok {
		t.Error("SourceFrom() found a source in an empty context")
	}

	src := &source.Source{ID: "acme"}
	ctx := withSource(t.Context(), src)

	got, ok := SourceFrom(ctx)
	if !ok {
		t.Fatal("SourceFrom() lost the source it was given")
	}
	if got.ID != "acme" {
		t.Errorf("ID = %q, want acme", got.ID)
	}
}
