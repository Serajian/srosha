package grpcsrv

import (
	"context"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
)

func whoamiSource() *source.Source {
	return &source.Source{
		ID:                 "01K0SRC0000000000000000000",
		Name:               "acme",
		MaxPriority:        shared.PriorityHigh,
		IsActive:           true,
		AllowCustomAddress: false,
		DefaultAddresses: map[shared.Channel]string{
			shared.ChannelEmail:    "ops@acme.test",
			shared.ChannelTelegram: "123456789",
		},
	}
}

func TestWhoamiAnswersWithTheCallerAndTheirLimits(t *testing.T) {
	s := NewSourceServer(Limits{
		Retention:          7 * 24 * time.Hour,
		RateLimitPerMinute: 600,
	})

	ctx := withSource(context.Background(), whoamiSource())

	got, err := s.Whoami(ctx, &pb.WhoamiRequest{})
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}

	if got.GetId() != "01K0SRC0000000000000000000" || got.GetName() != "acme" {
		t.Errorf("id/name = %q/%q", got.GetId(), got.GetName())
	}
	if got.GetMaxPriority() != pb.Priority_PRIORITY_HIGH {
		t.Errorf("max priority = %v, want high", got.GetMaxPriority())
	}
	if got.GetAllowCustomAddress() {
		t.Error("allow custom address came back true")
	}
	if got.GetRetention().AsDuration() != 7*24*time.Hour {
		t.Errorf("retention = %v, want 7 days", got.GetRetention().AsDuration())
	}
	if got.GetRateLimitPerMinute() != 600 {
		t.Errorf("rate limit = %d, want 600", got.GetRateLimitPerMinute())
	}

	// Keyed by channel name, because a proto3 map key cannot be an enum -- and
	// because a name survives this service learning a channel the caller's
	// build has never heard of.
	addresses := got.GetDefaultAddresses()
	if addresses["email"] != "ops@acme.test" || addresses["telegram"] != "123456789" {
		t.Errorf("default addresses = %v", addresses)
	}
}

// It reads the source the auth interceptor resolved and asks no repository, so
// reaching it without that interceptor is our wiring mistake and not the
// caller's.
func TestWhoamiWithoutAnAuthenticatedCaller(t *testing.T) {
	s := NewSourceServer(Limits{Retention: time.Hour, RateLimitPerMinute: 1})

	_, err := s.Whoami(context.Background(), &pb.WhoamiRequest{})
	if err == nil {
		t.Fatal("Whoami: want an error")
	}
	if !errs.IsType(err, errs.ErrInternal) {
		t.Errorf("error = %v, want internal -- the caller did nothing wrong", err)
	}
}

func TestASourceWithNoDefaultAddresses(t *testing.T) {
	s := NewSourceServer(Limits{Retention: time.Hour, RateLimitPerMinute: 1})

	src := whoamiSource()
	src.DefaultAddresses = nil

	got, err := s.Whoami(withSource(context.Background(), src), &pb.WhoamiRequest{})
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if len(got.GetDefaultAddresses()) != 0 {
		t.Errorf("default addresses = %v, want none", got.GetDefaultAddresses())
	}
}
