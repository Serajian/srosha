package shared_test

import (
	"encoding/json"
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

// AllChannels must stay in sync with the constants, or ValidateAddress's
// default branch is unreachable and a new channel would slip through untested.
func TestAllChannelsCoversEveryConstant(t *testing.T) {
	for _, c := range shared.AllChannels() {
		if !c.Valid() {
			t.Errorf("%q is listed in AllChannels but Valid() says otherwise", c)
		}
	}
	if got := len(shared.AllChannels()); got != 6 {
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

func TestValidateAddress(t *testing.T) {
	cases := []struct {
		name    string
		channel shared.Channel
		target  string
		wantErr error // nil means it should be accepted
	}{
		{"email plain", shared.ChannelEmail, "ops@example.com", nil},
		{"email with display name", shared.ChannelEmail, "Ops Team <ops@example.com>", nil},
		{"email missing at", shared.ChannelEmail, "ops.example.com", shared.ErrInvalidAddress},
		{"email is a phone number", shared.ChannelEmail, "+989121234567", shared.ErrInvalidAddress},

		{"telegram chat id", shared.ChannelTelegram, "123456789", nil},
		{"telegram group id is negative", shared.ChannelTelegram, "-1001234567890", nil},
		{
			"telegram chat id too large for an int64", shared.ChannelTelegram,
			"12345678901234567890123456", shared.ErrInvalidAddress,
		},
		{
			"telegram chat id with a stray sign",
			shared.ChannelTelegram,
			"--100",
			shared.ErrInvalidAddress,
		},

		{"telegram public channel name", shared.ChannelTelegram, "@acmenews", nil},
		{"telegram name too short", shared.ChannelTelegram, "@abcd", shared.ErrInvalidAddress},
		{
			"telegram name too long", shared.ChannelTelegram,
			"@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", shared.ErrInvalidAddress,
		},
		{
			"telegram name starting with a digit",
			shared.ChannelTelegram,
			"@1acme",
			shared.ErrInvalidAddress,
		},
		{
			"telegram name starting with an underscore",
			shared.ChannelTelegram,
			"@_acme",
			shared.ErrInvalidAddress,
		},
		{
			"telegram name ending with an underscore",
			shared.ChannelTelegram,
			"@acme_",
			shared.ErrInvalidAddress,
		},
		{
			"telegram name with a dash",
			shared.ChannelTelegram,
			"@acme-news",
			shared.ErrInvalidAddress,
		},
		{"telegram name with an underscore inside", shared.ChannelTelegram, "@acme_news", nil},
		{"telegram bare at", shared.ChannelTelegram, "@", shared.ErrInvalidAddress},
		{"bale follows the same rule", shared.ChannelBale, "@acmenews", nil},

		{"telegram group id", shared.ChannelTelegram, "-1001234567890", nil},
		{"telegram user id", shared.ChannelTelegram, "123456789", nil},
		{"telegram username", shared.ChannelTelegram, "@ops_channel", nil},
		{
			"telegram given an email",
			shared.ChannelTelegram,
			"ops@example.com",
			shared.ErrInvalidAddress,
		},
		{"telegram bare at", shared.ChannelTelegram, "@", shared.ErrInvalidAddress},
		{"telegram lone minus", shared.ChannelTelegram, "-", shared.ErrInvalidAddress},

		{"bale numeric", shared.ChannelBale, "123456789", nil},
		{"bale username", shared.ChannelBale, "@support", nil},

		{"whatsapp e164", shared.ChannelWhatsApp, "+989121234567", nil},
		{"whatsapp missing plus", shared.ChannelWhatsApp, "989121234567", shared.ErrInvalidAddress},
		{"whatsapp too short", shared.ChannelWhatsApp, "+1234567", shared.ErrInvalidAddress},
		{
			"whatsapp with spaces",
			shared.ChannelWhatsApp,
			"+98 912 123 4567",
			shared.ErrInvalidAddress,
		},

		{"whitespace only", shared.ChannelEmail, "   ", shared.ErrEmptyAddress},
		{"empty", shared.ChannelTelegram, "", shared.ErrEmptyAddress},
		{"unknown channel", shared.Channel("sms"), "123456789", shared.ErrUnknownChannel},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.channel.ValidateAddress(tc.target)

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
	err := shared.ChannelWhatsApp.ValidateAddress("989121234567")

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

// An unknown channel must not cross a wire in either direction: decoded
// quietly, it becomes a value every switch downstream has to guess at.
func TestChannelJSON(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		for _, want := range shared.AllChannels() {
			b, err := json.Marshal(want)
			if err != nil {
				t.Fatalf("Marshal(%v) error = %v", want, err)
			}

			var got shared.Channel
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", b, err)
			}
			if got != want {
				t.Errorf("round trip gave %q, want %q", got, want)
			}
		}
	})

	t.Run("refuses what it does not know", func(t *testing.T) {
		for _, in := range []string{`"carrier-pigeon"`, `""`, `"EMAIL"`, `1`, `null`} {
			var c shared.Channel
			if err := json.Unmarshal([]byte(in), &c); err == nil {
				t.Errorf("Unmarshal(%s) was accepted as %q", in, c)
			}
		}
	})

	t.Run("refuses to write what it does not know", func(t *testing.T) {
		if _, err := json.Marshal(shared.Channel("carrier-pigeon")); err == nil {
			t.Error("Marshal accepted a channel that is not one of the four")
		}
	})
}

// Google promises nothing about a device token's shape, so the rule is only a
// length: a shape invented here would one day refuse a token that works.
func TestAnFCMAddressIsOnlyCheckedForLength(t *testing.T) {
	good := []string{
		strings.Repeat("a", 32),
		strings.Repeat("z", 163),
		"cXy_dE:APA91bH" + strings.Repeat("Q", 140),
	}
	for _, address := range good {
		if err := shared.ChannelFCM.ValidateAddress(address); err != nil {
			t.Errorf("ValidateAddress(%d chars) = %v, want it accepted", len(address), err)
		}
	}

	bad := map[string]string{
		"empty":     "",
		"too short": "not-a-device-token",
		"absurd":    strings.Repeat("a", 5000),
	}
	for name, address := range bad {
		t.Run(name, func(t *testing.T) {
			if err := shared.ChannelFCM.ValidateAddress(address); err == nil {
				t.Errorf("ValidateAddress(%q) = nil, want a refusal", name)
			}
		})
	}
}

// Matrix has no way to message a person: you write into a room. A user id would
// be accepted here and then fail on every send, so it is refused where it can
// still be reported as a mistake.
func TestAMatrixAddressIsARoom(t *testing.T) {
	good := []string{"!abcdef:matrix.org", "!x:example.test", "!a:b"}
	for _, address := range good {
		if err := shared.ChannelMatrix.ValidateAddress(address); err != nil {
			t.Errorf("ValidateAddress(%q) = %v", address, err)
		}
	}

	bad := map[string]string{
		"a user":         "@someone:matrix.org",
		"an alias":       "#general:matrix.org",
		"no sigil":       "abcdef:matrix.org",
		"no server":      "!abcdef",
		"no local part":  "!:matrix.org",
		"nothing at all": "",
		"an email":       "someone@acme.test",
	}
	for name, address := range bad {
		t.Run(name, func(t *testing.T) {
			if err := shared.ChannelMatrix.ValidateAddress(address); err == nil {
				t.Errorf("ValidateAddress(%q) was accepted", address)
			}
		})
	}
}
