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
