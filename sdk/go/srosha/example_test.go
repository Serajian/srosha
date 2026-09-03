package srosha_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Serajian/srosha/sdk/go/srosha"
)

// The whole of the daily path: connect once, send, ask what happened.
func Example() {
	ctx := context.Background()

	c, err := srosha.New(ctx, "srosha.acme.test:443", "srosha_your-key")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	r, err := c.Submit(ctx, srosha.Message{
		IdempotencyKey: "order-42",
		Title:          "Your order shipped",
		Body:           "Tracking: 123",
		Routes: []srosha.Route{
			srosha.EmailTo("a@b.test"),
			srosha.TelegramTo("123456789").From("alerts"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	got, err := c.Get(ctx, r.ID)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range got.Deliveries {
		fmt.Println(d.Channel, d.Status, d.Reason)
	}
}

// A caller inside srosha's own network, where the service listens without TLS.
// It has to be said out loud: the default is encrypted.
func ExampleWithInsecure() {
	c, err := srosha.New(
		context.Background(), "gateway:50051", "srosha_your-key",
		srosha.WithInsecure(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()
}

// Errors are tested with errors.Is, never by reading the message. The message
// is srosha's own words, for a person.
func ExampleError() {
	c, _ := srosha.New(context.Background(), "srosha.acme.test:443", "srosha_your-key")
	defer func() { _ = c.Close() }()

	_, err := c.Submit(context.Background(), srosha.Message{
		Body:   "hello",
		Routes: []srosha.Route{srosha.EmailTo("not-an-address")},
	})
	switch {
	case err == nil:
	case errors.Is(err, srosha.ErrInvalidRequest):
		fmt.Println("fix the request:", err)
	case errors.Is(err, srosha.ErrRateLimited):
		fmt.Println("slow down")
	default:
		log.Fatal(err)
	}
}

// Listing walks pages as the loop asks for them, so breaking out stops the
// requests.
func ExampleClient_List() {
	ctx := context.Background()

	c, _ := srosha.New(ctx, "srosha.acme.test:443", "srosha_your-key")
	defer func() { _ = c.Close() }()

	cutoff := time.Now().Add(-time.Hour)
	for n, err := range c.List(ctx, srosha.LastDay) {
		if err != nil {
			log.Fatal(err)
		}
		if n.CreatedAt.Before(cutoff) {
			break
		}
		fmt.Println(n.ID, n.Title)
	}
}

// A source registers each identity once. After this, Submit names a channel
// and not an identity.
func ExampleCredentials_Register() {
	ctx := context.Background()

	c, _ := srosha.New(ctx, "srosha.acme.test:443", "srosha_your-key")
	defer func() { _ = c.Close() }()

	// A bot is a token and nothing else.
	_, err := c.Credentials.Register(ctx, srosha.Registration{
		Name:       "alerts",
		Default:    true,
		Credential: srosha.TelegramCredential{Token: "111:your-bot-token"},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Mail is a whole account.
	_, err = c.Credentials.Register(ctx, srosha.Registration{
		Name:    "mail",
		Default: true,
		Credential: srosha.SMTPCredential{
			Host: "smtp.acme.test", Port: 587,
			Username: "bot", From: "bot@acme.test", Password: "your-password",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Apple wants four things, and only the key is secret. An unset
	// environment means production, which is what a shipped app uses.
	_, err = c.Credentials.Register(ctx, srosha.Registration{
		Name:    "ios",
		Default: true,
		Credential: srosha.APNsCredential{
			KeyID: "ABC1234567", TeamID: "TEAM123456",
			Topic: "com.acme.app", Environment: srosha.APNsSandbox,
			Key: "-----BEGIN PRIVATE KEY-----\n…\n-----END PRIVATE KEY-----\n",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// A leaked token needs the secret replaced and the name kept: registering a
// second identity instead would make every message still naming the old one
// fail, turning a leak into a code change.
func ExampleCredentials_Rotate() {
	ctx := context.Background()

	c, _ := srosha.New(ctx, "srosha.acme.test:443", "srosha_your-key")
	defer func() { _ = c.Close() }()

	identities, err := c.Credentials.List(ctx, srosha.ChannelTelegram)
	if err != nil {
		log.Fatal(err)
	}

	for _, id := range identities {
		if id.Name != "alerts" {
			continue
		}
		if _, err := c.Credentials.Rotate(ctx, id.ID, "111:the-new-token"); err != nil {
			log.Fatal(err)
		}
	}
}

// Registering a webhook says where outcomes are pushed. Verifying the signature
// on what arrives is not done for you -- see the note on Webhooks.
func ExampleWebhooks_Register() {
	ctx := context.Background()

	c, _ := srosha.New(ctx, "srosha.acme.test:443", "srosha_your-key")
	defer func() { _ = c.Close() }()

	h, secret, err := c.Webhooks.Register(ctx, "https://acme.test/srosha")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(h.CallbackURL, h.Active)

	// The only time anything hands it to you. Store it wherever your verifier
	// reads its secret from -- srosha keeps it encrypted and will not repeat
	// it. Empty here would mean this call moved an address rather than
	// creating a callback.
	if secret != "" {
		fmt.Println("store this:", secret)
	}
}

// Deliveries say what happened per recipient. Not every failure is the same
// kind of failure.
func ExampleClient_Get() {
	ctx := context.Background()

	c, _ := srosha.New(ctx, "srosha.acme.test:443", "srosha_your-key")
	defer func() { _ = c.Close() }()

	got, err := c.Get(ctx, "01M11DF6WBFTHMAHMEFS1WV08S")
	if err != nil {
		log.Fatal(err)
	}

	for _, d := range got.Deliveries {
		switch d.Reason {
		case srosha.FailureNone:
		case srosha.FailureNotReachable:
			// The provider refused the recipient, not the message. Nothing
			// written differently would have helped: stop sending there.
			fmt.Println("stop sending to", d.Address)
		case srosha.FailureNoSender:
			// No identity is configured for that channel. Ours to fix.
			fmt.Println("register an identity for", d.Channel)
		default:
			fmt.Println(d.Channel, "failed:", d.Reason)
		}
	}
}

// Whoami at startup answers two things that are otherwise only learnable by
// getting them wrong.
func ExampleClient_Whoami() {
	ctx := context.Background()

	c, err := srosha.New(ctx, "api.srosha.acme.test", "srosha_your-key")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	me, err := c.Whoami(ctx)
	if err != nil {
		// Not a reason to refuse to start: srosha is asynchronous, and an
		// application that will not boot while it is briefly down is worse
		// than one that says so and carries on.
		log.Println("srosha unreachable at startup:", err)
		return
	}

	fmt.Println("sending as", me.Name)
	fmt.Println("priority ceiling", me.MaxPriority)
	fmt.Println("history goes back", me.MaxWindow())

	if !me.AllowCustomAddress {
		fmt.Println("addresses are fixed:", me.DefaultAddresses)
	}
}

// Verifying a callback before trusting it. Without this, an endpoint accepts
// whatever anybody posts to that url.
func ExampleVerifier_Verify() {
	// Once, at startup. The secret is handed to you out of band; srosha never
	// returns it from any rpc.
	v, err := srosha.NewVerifier(os.Getenv("SROSHA_WEBHOOK_SECRET"))
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/srosha", func(w http.ResponseWriter, r *http.Request) {
		// Read the body raw and hand it over unchanged: the signature covers
		// the exact bytes srosha sent.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		cb, err := v.Verify(r.Header, body)
		if err != nil {
			// Do not say which check failed. Whoever is guessing does not need
			// to be told how close they got.
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		for _, d := range cb.Deliveries {
			switch {
			case d.Status == srosha.StatusSent:
				fmt.Println("delivered", d.Channel, d.ProviderMessageID)
			case d.Reason == srosha.FailureNotReachable:
				fmt.Println("stop sending to", d.Address)
			default:
				fmt.Println("failed", d.Channel, d.Reason)
			}
		}

		// Answer 2xx once you have it. srosha stops retrying at a limit and
		// switches a dead endpoint off.
		w.WriteHeader(http.StatusOK)
	})
}

// Every credential in the README's "What each channel needs", so that section
// cannot rot without `go test ./...` saying so.
//
// A wrong field name in an SDK's own documentation is the worst kind: the
// reader copies it and meets the compiler, not us. These were checked by hand
// once, on 2026-09-01, which is exactly the sort of checking that stops
// happening. Keep the two in step -- if you change a snippet there, change it
// here, and if this stops compiling, that section is already wrong.
func ExampleCredentials_Register_everyChannel() {
	// Telegram and Bale: one token, from BotFather, and optionally a parse
	// mode. srosha does not escape, and Telegram refuses what it cannot parse.
	_ = srosha.TelegramCredential{Token: "123456:AAH…"}
	_ = srosha.BaleCredential{Token: "123456:AAH…", ParseMode: "HTML"}

	// Email: a server, an account and an address. Port 465 is TLS from the
	// first byte, anything else is STARTTLS, and zero means 587.
	_ = srosha.SMTPCredential{
		Host:        "smtp.acme.test",
		Port:        587,
		Username:    "noreply@acme.test",
		From:        "noreply@acme.test",
		Password:    "your-password",
		ContentType: "text/html",
	}

	// Matrix: https, and a bare address -- no path, no credentials in the url.
	_ = srosha.MatrixCredential{
		Homeserver: "https://matrix.acme.test",
		Token:      "your-access-token",
	}

	// Gotify: the same shape, and an application token rather than a client
	// one. It alone decides which application a message lands in.
	// ContentType is safe here in a way ParseMode is not: Gotify shows markup
	// it cannot parse rather than refusing the message.
	_ = srosha.GotifyCredential{
		ServerURL:   "https://gotify.acme.test",
		Token:       "your-app-token",
		ContentType: "text/markdown",
	}

	// WhatsApp: Meta's id for the number, which is not the number.
	_ = srosha.WhatsAppCredential{
		PhoneNumberID: "109876543210987",
		Token:         "your-access-token",
	}

	// FCM: the whole service account json file, as it came. Not base64 of it,
	// and not a path to it.
	key, err := os.ReadFile("service-account.json")
	if err != nil {
		log.Fatal(err)
	}
	_ = srosha.FCMCredential{ServiceAccount: string(key)}

	// APNs: four values and a file. An unset Environment means production,
	// which is what a shipped app uses -- a token from a development build is
	// unknown there and comes back as FailureNotReachable.
	p8, err := os.ReadFile("AuthKey_ABC123DEFG.p8")
	if err != nil {
		log.Fatal(err)
	}
	_ = srosha.APNsCredential{
		Key:    string(p8),
		KeyID:  "ABC123DEFG",
		TeamID: "DEF456GHIJ",
		Topic:  "test.acme.app",
	}

	// Raw: a channel this build has no type for yet.
	_ = srosha.RawCredential{
		Channel: srosha.Channel("something-new"),
		Config:  `{"endpoint":"https://acme.test"}`,
		Secret:  "your-token",
	}
}

// Every send in the README's "One of each", for the same reason as above: the
// section is only true for as long as something compiles it.
func ExampleClient_Submit_everyChannel() {
	ctx := context.Background()

	c, _ := srosha.New(ctx, "srosha.acme.test:443", "srosha_your-key")
	defer func() { _ = c.Close() }()

	// Telegram and Bale: a chat id. Title and Body arrive as one text with a
	// blank line between them.
	_, _ = c.Submit(ctx, srosha.Message{
		Title:  "Deploy finished",
		Body:   "srosha 1.4.0 is live.",
		Routes: []srosha.Route{srosha.TelegramTo("123456789")},
	})
	_, _ = c.Submit(ctx, srosha.Message{
		Body:   "The service is up.",
		Routes: []srosha.Route{srosha.BaleTo("-100123456789")},
	})

	// Email: Title is the subject line.
	_, _ = c.Submit(ctx, srosha.Message{
		Title:  "Your order shipped",
		Body:   "Tracking: 1Z999.",
		Routes: []srosha.Route{srosha.EmailTo("someone@acme.test")},
	})

	// Gotify: the address is a positive integer that Gotify ignores and srosha
	// does not. Two Gotify routes in one message need different ones, or the
	// second folds into the first as a duplicate.
	_, _ = c.Submit(ctx, srosha.Message{
		Title: "Disk is running out",
		Body:  "4.0 GB free of 96.0 GB at /",
		Routes: []srosha.Route{
			srosha.GotifyTo("1").From("ops"),
			srosha.GotifyTo("2").From("oncall"),
		},
	})

	// Matrix: a room id, never a user id.
	_, _ = c.Submit(ctx, srosha.Message{
		Body:   "Build 412 failed.",
		Routes: []srosha.Route{srosha.MatrixTo("!abcdef:matrix.acme.test")},
	})

	// WhatsApp: outside a window the recipient opened, an approved template
	// goes instead of your text.
	_, _ = c.Submit(ctx, srosha.Message{
		Body: "Your order shipped.",
		Metadata: map[string]string{
			"template":        "order_shipped",
			"template_params": `["Ali","123"]`,
		},
		Routes: []srosha.Route{srosha.WhatsAppTo("+989121234567")},
	})

	// FCM: the whole Metadata map becomes the push's data payload.
	_, _ = c.Submit(ctx, srosha.Message{
		Title:    "New message",
		Body:     "Ali sent you a photo.",
		Metadata: map[string]string{"thread_id": "42"},
		Routes:   []srosha.Route{srosha.FCMTo("your-device-token")},
	})

	// APNs: the map sits beside Apple's own aps key.
	_, _ = c.Submit(ctx, srosha.Message{
		Title:    "New message",
		Body:     "Ali sent you a photo.",
		Metadata: map[string]string{"thread_id": "42"},
		Routes:   []srosha.Route{srosha.APNsTo("a1b2c3d4")},
	})

	// All of them at once, which is the point of Routes being a list: one
	// message, one idempotency key, one place to ask what happened to each.
	_, _ = c.Submit(ctx, srosha.Message{
		IdempotencyKey: "incident-4711",
		Title:          "Database unreachable",
		Body:           "Retrying since 03:12.",
		Priority:       srosha.PriorityHigh,
		Routes: []srosha.Route{
			srosha.EmailTo("oncall@acme.test"),
			srosha.TelegramTo("-100123456789"),
			srosha.GotifyTo("1"),
			srosha.Matrix(),
		},
	})
}
