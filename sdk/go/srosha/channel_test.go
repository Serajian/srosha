package srosha_test

import (
	"testing"

	"github.com/Serajian/srosha/sdk/go/srosha"
)

// The bare form is the whole reason for the split: it must actually leave
// Address empty, or "the default" would still have to be spelled some other
// way.
func TestTheBareFormLeavesTheAddressEmpty(t *testing.T) {
	cases := map[string]srosha.Route{
		"Email":    srosha.Email(),
		"Telegram": srosha.Telegram(),
		"Bale":     srosha.Bale(),
		"WhatsApp": srosha.WhatsApp(),
		"Matrix":   srosha.Matrix(),
		"Gotify":   srosha.Gotify(),
		"FCM":      srosha.FCM(),
		"APNs":     srosha.APNs(),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			if r.Address != "" {
				t.Errorf("%s() Address = %q, want empty", name, r.Address)
			}
		})
	}
}

// The To form carries the address it was given, unchanged.
func TestTheToFormCarriesTheGivenAddress(t *testing.T) {
	cases := map[string]srosha.Route{
		"EmailTo":    srosha.EmailTo("a@b.test"),
		"TelegramTo": srosha.TelegramTo("123456789"),
		"BaleTo":     srosha.BaleTo("123456789"),
		"WhatsAppTo": srosha.WhatsAppTo("+989121234567"),
		"MatrixTo":   srosha.MatrixTo("!abc:matrix.org"),
		"GotifyTo":   srosha.GotifyTo("42"),
		"FCMTo":      srosha.FCMTo("device-token"),
		"APNsTo":     srosha.APNsTo("a1b2c3"),
	}
	want := map[string]string{
		"EmailTo":    "a@b.test",
		"TelegramTo": "123456789",
		"BaleTo":     "123456789",
		"WhatsAppTo": "+989121234567",
		"MatrixTo":   "!abc:matrix.org",
		"GotifyTo":   "42",
		"FCMTo":      "device-token",
		"APNsTo":     "a1b2c3",
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			if r.Address != want[name] {
				t.Errorf("%s Address = %q, want %q", name, r.Address, want[name])
			}
		})
	}
}

// From chains off either form -- a route built for the default address and
// one built for a named address both end up carrying the sender.
func TestFromChainsOffBothTheBareAndToForms(t *testing.T) {
	bare := srosha.Telegram().From("alerts")
	if bare.Sender != "alerts" {
		t.Errorf("Telegram().From(...) Sender = %q, want alerts", bare.Sender)
	}
	if bare.Address != "" {
		t.Errorf("Telegram().From(...) Address = %q, want empty", bare.Address)
	}

	explicit := srosha.TelegramTo("123456789").From("alerts")
	if explicit.Sender != "alerts" {
		t.Errorf("TelegramTo(...).From(...) Sender = %q, want alerts", explicit.Sender)
	}
	if explicit.Address != "123456789" {
		t.Errorf("TelegramTo(...).From(...) Address = %q, want 123456789", explicit.Address)
	}
}
