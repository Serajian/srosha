package sender_test

import (
	"testing"

	"github.com/Serajian/srosha/internal/adapter/sender/apns"
	"github.com/Serajian/srosha/internal/adapter/sender/email"
	"github.com/Serajian/srosha/internal/adapter/sender/gotify"
	"github.com/Serajian/srosha/internal/adapter/sender/matrix"
	"github.com/Serajian/srosha/internal/adapter/sender/whatsapp"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/sdk/go/srosha"
)

// The seam this file exists for.
//
// The SDK builds a settings document for each channel, and the service parses
// one. Nothing else proves the two agree: the SDK cannot import this package --
// the dependency runs the other way -- so if it could not import the SDK
// either, a field renamed on one side would be found by a customer, on a real
// message, days later.
//
// This is the concrete payoff of keeping the client and the service in one
// repository. A separate repository could not have this test at all.
//
// Three channels are absent on purpose. Telegram, Bale and FCM have no settings
// at all -- their whole credential is the secret -- so there is nothing here
// that could disagree.
func TestEverySDKCredentialParsesOnThisSide(t *testing.T) {
	cases := []struct {
		name  string
		cred  srosha.Credential
		parse func(*testing.T, []byte)
	}{
		{
			name: "email",
			cred: srosha.SMTPCredential{
				Host: "smtp.acme.test", Port: 587,
				Username: "bot", From: "bot@acme.test", Password: "s3cret",
			},
			parse: func(t *testing.T, raw []byte) {
				cfg, err := email.ParseConfig(raw)
				if err != nil {
					t.Fatalf("email.ParseConfig: %v", err)
				}
				if cfg.Host != "smtp.acme.test" || cfg.Port != 587 {
					t.Errorf("parsed %+v, want the sdk's values", cfg)
				}
				if cfg.From != "bot@acme.test" || cfg.Username != "bot" {
					t.Errorf("parsed %+v, want the sdk's values", cfg)
				}
			},
		},
		{
			name: "matrix",
			cred: srosha.MatrixCredential{
				Homeserver: "https://matrix.acme.test", Token: "syt-x",
			},
			parse: func(t *testing.T, raw []byte) {
				cfg, err := matrix.ParseConfig(raw)
				if err != nil {
					t.Fatalf("matrix.ParseConfig: %v", err)
				}
				if cfg.Homeserver != "https://matrix.acme.test" {
					t.Errorf("homeserver = %q, want the sdk's", cfg.Homeserver)
				}
			},
		},
		{
			name: "gotify",
			cred: srosha.GotifyCredential{
				ServerURL: "https://gotify.acme.test", Token: "AbCdEf.token",
			},
			parse: func(t *testing.T, raw []byte) {
				cfg, err := gotify.ParseConfig(raw)
				if err != nil {
					t.Fatalf("gotify.ParseConfig: %v", err)
				}
				if cfg.ServerURL != "https://gotify.acme.test" {
					t.Errorf("server url = %q, want the sdk's", cfg.ServerURL)
				}
			},
		},
		{
			name: "whatsapp",
			cred: srosha.WhatsAppCredential{PhoneNumberID: "123456789", Token: "EAAG"},
			parse: func(t *testing.T, raw []byte) {
				cfg, err := whatsapp.ParseConfig(raw)
				if err != nil {
					t.Fatalf("whatsapp.ParseConfig: %v", err)
				}
				if cfg.PhoneNumberID != "123456789" {
					t.Errorf("phone number id = %q, want the sdk's", cfg.PhoneNumberID)
				}
			},
		},
		{
			name: "apns",
			cred: srosha.APNsCredential{
				KeyID: "ABC1234567", TeamID: "TEAM123456",
				Topic: "com.acme.app", Environment: srosha.APNsSandbox, Key: "p8",
			},
			parse: func(t *testing.T, raw []byte) {
				cfg, err := apns.ParseConfig(raw)
				if err != nil {
					t.Fatalf("apns.ParseConfig: %v", err)
				}
				if cfg.KeyID != "ABC1234567" || cfg.TeamID != "TEAM123456" {
					t.Errorf("parsed %+v, want the sdk's values", cfg)
				}
				if cfg.Topic != "com.acme.app" || cfg.Environment != "sandbox" {
					t.Errorf("parsed %+v, want the sdk's values", cfg)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			settings, err := c.cred.Settings()
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			c.parse(t, []byte(settings))
		})
	}
}

// Leaving Environment unset must reach the service as production, because that
// is what a shipped app uses -- and getting it wrong looks like a dead device
// rather than a wrong address.
func TestAnUnsetAPNsEnvironmentIsProduction(t *testing.T) {
	settings, err := srosha.APNsCredential{
		KeyID: "ABC1234567", TeamID: "TEAM123456", Topic: "com.acme.app",
	}.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}

	cfg, err := apns.ParseConfig([]byte(settings))
	if err != nil {
		t.Fatalf("apns.ParseConfig: %v", err)
	}
	if cfg.Environment != "production" {
		t.Errorf("environment = %q, want production", cfg.Environment)
	}
}

// The channel names have to be the same word on both sides, or a source
// registers on one channel and sends on another.
func TestTheChannelNamesMatch(t *testing.T) {
	cases := map[srosha.Channel]shared.Channel{
		srosha.ChannelEmail:    shared.ChannelEmail,
		srosha.ChannelTelegram: shared.ChannelTelegram,
		srosha.ChannelBale:     shared.ChannelBale,
		srosha.ChannelWhatsApp: shared.ChannelWhatsApp,
		srosha.ChannelMatrix:   shared.ChannelMatrix,
		srosha.ChannelGotify:   shared.ChannelGotify,
		srosha.ChannelFCM:      shared.ChannelFCM,
		srosha.ChannelAPNs:     shared.ChannelAPNs,
	}
	if len(cases) != len(shared.AllChannels()) {
		t.Fatalf("the sdk names %d channels, the service has %d",
			len(cases), len(shared.AllChannels()))
	}
	for sdk, service := range cases {
		if string(sdk) != string(service) {
			t.Errorf("sdk calls it %q, the service calls it %q", sdk, service)
		}
	}
}
