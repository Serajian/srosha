package shared_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

func TestParseChannel(t *testing.T) {
	for _, c := range shared.AllChannels() {
		got, err := shared.ParseChannel(c.String())
		if err != nil {
			t.Fatalf("ParseChannel(%q): %v", c, err)
		}
		if got != c {
			t.Errorf("got %q, want %q", got, c)
		}
	}

	for _, bad := range []string{"telegran", "EMAIL", "", "sms"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			_, err := shared.ParseChannel(bad)
			if !errors.Is(err, shared.ErrUnknownChannel) {
				t.Errorf("error = %v, want it to wrap ErrUnknownChannel", err)
			}
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("type = %v, want ErrInvalidInput", errs.TypeOf(err))
			}
		})
	}
}

// AllChannels must stay in sync with the constants, or ValidateTarget's
// default branch is unreachable and a new channel would slip through untested.
func TestAllChannelsCoversEveryConstant(t *testing.T) {
	for _, c := range shared.AllChannels() {
		if !c.Valid() {
			t.Errorf("%q is listed in AllChannels but Valid() says otherwise", c)
		}
	}
	if got := len(shared.AllChannels()); got != 4 {
		t.Errorf("AllChannels has %d entries; update this test when adding a channel", got)
	}
}

func TestAllChannelsReturnsAFreshSlice(t *testing.T) {
	first := shared.AllChannels()
	first[0] = "tampered"

	if shared.AllChannels()[0] != shared.ChannelEmail {
		t.Error("callers share one backing array; mutating it corrupts everyone")
	}
}

func TestValidateTarget(t *testing.T) {
	cases := []struct {
		name    string
		channel shared.Channel
		target  string
		wantErr error // nil means it should be accepted
	}{
		{"email plain", shared.ChannelEmail, "ops@example.com", nil},
		{"email with display name", shared.ChannelEmail, "Ops Team <ops@example.com>", nil},
		{"email missing at", shared.ChannelEmail, "ops.example.com", shared.ErrInvalidTarget},
		{"email is a phone number", shared.ChannelEmail, "+989121234567", shared.ErrInvalidTarget},

		{"telegram group id", shared.ChannelTelegram, "-1001234567890", nil},
		{"telegram user id", shared.ChannelTelegram, "123456789", nil},
		{"telegram username", shared.ChannelTelegram, "@ops_channel", nil},
		{"telegram given an email", shared.ChannelTelegram, "ops@example.com", shared.ErrInvalidTarget},
		{"telegram bare at", shared.ChannelTelegram, "@", shared.ErrInvalidTarget},
		{"telegram lone minus", shared.ChannelTelegram, "-", shared.ErrInvalidTarget},

		{"bale numeric", shared.ChannelBale, "123456789", nil},
		{"bale username", shared.ChannelBale, "@support", nil},

		{"whatsapp e164", shared.ChannelWhatsApp, "+989121234567", nil},
		{"whatsapp missing plus", shared.ChannelWhatsApp, "989121234567", shared.ErrInvalidTarget},
		{"whatsapp too short", shared.ChannelWhatsApp, "+1234567", shared.ErrInvalidTarget},
		{"whatsapp with spaces", shared.ChannelWhatsApp, "+98 912 123 4567", shared.ErrInvalidTarget},

		{"whitespace only", shared.ChannelEmail, "   ", shared.ErrEmptyTarget},
		{"empty", shared.ChannelTelegram, "", shared.ErrEmptyTarget},
		{"unknown channel", shared.Channel("sms"), "123456789", shared.ErrUnknownChannel},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.channel.ValidateTarget(tc.target)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want it to wrap %v", err, tc.wantErr)
			}
		})
	}
}

// The rejected value is useful in a log and dangerous in a response: it can be
// someone's phone number or address, and it tells a caller how our formats work.
func TestInvalidTargetMessageHidesTheValue(t *testing.T) {
	err := shared.ChannelWhatsApp.ValidateTarget("989121234567")

	ae, ok := errs.As(err)
	if !ok {
		t.Fatal("not an AppError")
	}
	if strings.Contains(ae.Message(), "989121234567") {
		t.Errorf("message echoes the target back: %q", ae.Message())
	}
	if strings.Contains(ae.Message(), "E.164") {
		t.Errorf("message leaks the accepted format: %q", ae.Message())
	}
	if ae.Reason() == nil || !strings.Contains(ae.Reason().Error(), "989121234567") {
		t.Errorf("reason should keep the value for debugging: %v", ae.Reason())
	}
}
