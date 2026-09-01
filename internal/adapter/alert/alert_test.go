package alert_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/alert"
	"github.com/Serajian/srosha/internal/core/shared"
)

// blocked never answers until it is let go.
type blocked struct{ release chan struct{} }

func (b *blocked) Send(context.Context, shared.Message) (string, error) {
	<-b.release
	return "", nil
}

// A push server that has stopped answering must not stop srosha.
//
// This is the whole point of the package. With the worker stuck and the queue
// full, Notify has to return anyway: a source registering cannot be made to
// wait on Gotify, and must not be able to be.
func TestNotifyDoesNotBlockWhenTheQueueIsFull(t *testing.T) {
	p := &blocked{release: make(chan struct{})}
	a := alert.New(p, "42", alert.Config{Queue: 1, Timeout: time.Second}, quiet())

	// Order matters, and getting it wrong hangs the test rather than failing
	// it: Close waits for the worker, and the worker is inside Send until the
	// release is closed. Defers run last-registered-first, so the release has
	// to be registered after Close.
	defer func() { _ = a.Close(context.Background()) }()
	defer close(p.release)

	done := make(chan struct{})
	go func() {
		for range 50 {
			a.Notify(context.Background(), "subject", "detail")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked: a stuck push server would stall whatever called it")
	}
}

type failing struct{}

func (failing) Send(context.Context, shared.Message) (string, error) {
	return "", errors.New("gotify is down")
}

// A pusher that always fails changes nothing for the caller. Notify returns
// nothing at all, which is the point: there is no error for anyone to mishandle.
func TestAFailingPusherIsInvisibleToTheCaller(t *testing.T) {
	a := alert.New(failing{}, "42", alert.Config{Queue: 4, Timeout: time.Second}, quiet())

	a.Notify(context.Background(), "subject", "detail")

	if err := a.Close(context.Background()); err != nil {
		t.Errorf("Close after a failed send = %v, want nil", err)
	}
}

// captor keeps what was actually sent, so the wording is checked rather than
// assumed.
type captor struct{ got chan shared.Message }

func (c captor) Send(_ context.Context, m shared.Message) (string, error) {
	c.got <- m
	return "1", nil
}

func TestTheMessageCarriesTheSubjectAndTheDetail(t *testing.T) {
	c := captor{got: make(chan shared.Message, 1)}
	a := alert.New(c, "42", alert.Config{Queue: 4, Timeout: time.Second}, quiet())
	defer func() { _ = a.Close(context.Background()) }()

	a.Notify(context.Background(), "source.create", "someone@acme.test registered 01K0")

	select {
	case m := <-c.got:
		if m.Recipient.Address != "42" {
			t.Errorf("address = %q, want the configured application", m.Recipient.Address)
		}
		if m.Recipient.Channel != shared.ChannelGotify {
			t.Errorf("channel = %q, want gotify", m.Recipient.Channel)
		}
		if m.Title == "" || m.Body == "" {
			t.Errorf("title = %q, body = %q -- both are read by a person", m.Title, m.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was sent")
	}
}

// Unconfigured is a no-op, so a laptop pays nothing for a feature it cannot use.
func TestANilPusherIsSilentAndCostsNothing(t *testing.T) {
	a := alert.New(nil, "", alert.Config{Queue: 4, Timeout: time.Second}, quiet())

	a.Notify(context.Background(), "subject", "detail")

	if err := a.Close(context.Background()); err != nil {
		t.Errorf("Close on a no-op = %v, want nil", err)
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
