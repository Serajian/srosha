package srosha_test

import (
	"context"
	"errors"
	"fmt"
	"log"
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
			srosha.Email("a@b.test"),
			srosha.Telegram("123456789").From("alerts"),
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
		Routes: []srosha.Route{srosha.Email("not-an-address")},
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

	h, err := c.Webhooks.Register(ctx, "https://acme.test/srosha")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(h.CallbackURL, h.Active)
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
