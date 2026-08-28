package mailer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/infra/smtp"
)

const from = "srosha@acme.test"

// A stand-in cannot intercept the send -- Open returns a concrete client -- so
// it covers the one path it can reach.
type dialer struct {
	opened int
	err    error
}

func (d *dialer) Open(smtp.Identity) (*smtp.Client, error) {
	d.opened++
	return nil, d.err
}

func identity() smtp.Identity {
	return smtp.Identity{
		Host: "smtp.acme.test", Port: 587,
		Username: "srosha", Password: "pw",
	}
}

func TestTheCodeIsInTheMessage(t *testing.T) {
	msg := compose(from, "ops@acme.test", "482913")

	if msg.To != "ops@acme.test" || msg.From != from {
		t.Errorf("addresses = %q -> %q", msg.From, msg.To)
	}
	if !strings.Contains(msg.Body, "482913") {
		t.Errorf("body has no code in it:\n%s", msg.Body)
	}
	if msg.Subject == "" {
		t.Error("no subject")
	}
	if msg.ContentType != "text/plain" {
		t.Errorf("content type = %q, want text/plain", msg.ContentType)
	}
}

// Somebody who did not ask for this has to be told that nothing has happened,
// or a code arriving out of nowhere reads as a break-in.
func TestTheMessageSaysWhatToDoIfYouDidNotAskForIt(t *testing.T) {
	body := compose(from, "ops@acme.test", "482913").Body

	if !strings.Contains(body, "did not ask") {
		t.Errorf("body does not reassure an unexpected recipient:\n%s", body)
	}
}

// The code must not turn up in the subject, where it shows in a notification
// on a locked screen.
func TestTheCodeIsNotInTheSubject(t *testing.T) {
	msg := compose(from, "ops@acme.test", "482913")

	if strings.Contains(msg.Subject, "482913") {
		t.Errorf("subject = %q, and it carries the code", msg.Subject)
	}
}

func TestAMailServerWeCannotReach(t *testing.T) {
	d := &dialer{err: errors.New("dial tcp: connection refused")}

	m, err := New(d, identity(), from)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := m.SendCode(context.Background(), "ops@acme.test", "482913"); err == nil {
		t.Fatal("SendCode: want an error")
	}
	if d.opened != 1 {
		t.Errorf("opened %d times, want 1", d.opened)
	}
}

// The address a person replies to is not the account we authenticate as, so it
// is its own argument -- and a mailer with neither half is refused rather than
// sending from an empty From.
func TestAMailerNeedsADialerAndAnAddress(t *testing.T) {
	if _, err := New(nil, identity(), from); err == nil {
		t.Error("New with no dialer succeeded")
	}
	if _, err := New(&dialer{}, identity(), "  "); err == nil {
		t.Error("New with no from address succeeded")
	}
}
