package email_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/sender/email"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/infra/smtp"
	"github.com/Serajian/srosha/pkg/errs"
)

// coded stands in for what infra returns: an error carrying a reply code. What
// this package decides from that code is the whole of what it is tested on --
// the conversation with a server is infra's, and tested there against a real
// one.
type coded struct{ code int }

func (c coded) Error() string  { return "smtp: send: refused" }
func (c coded) ReplyCode() int { return c.code }

type dialer struct {
	err    error
	opened int
}

func (d *dialer) Open(smtp.Identity) (*smtp.Client, error) {
	d.opened++
	return nil, d.err
}

func config() email.Config {
	return email.Config{
		Host: "smtp.acme.test", Port: 587, Username: "acme", From: "srosha@acme.test",
	}
}

func newSender(t *testing.T, d *dialer) *email.Sender {
	t.Helper()

	s, err := email.New(d, config(), "a-password")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func msg() shared.Message {
	return shared.Message{
		Recipient: shared.Recipient{Channel: shared.ChannelEmail, Address: "someone@acme.test"},
		Title:     "Deploy",
		Body:      "it is done",
	}
}

// SMTP answers the retry question itself, in the first digit of every reply.
func TestWhatIsWorthAnotherAttempt(t *testing.T) {
	tests := map[string]struct {
		code      int
		permanent bool
	}{
		"no such mailbox":        {550, true},
		"relay denied":           {554, true},
		"message refused":        {552, true},
		"credentials refused":    {535, true},
		"greylisted":             {450, false},
		"service not available":  {421, false},
		"mailbox full for now":   {452, false},
		"never reached a server": {0, false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// The identity is checked at registration, so the dialer works
			// while the sender is built and starts failing afterwards -- which
			// is the shape of every failure a real one has.
			d := &dialer{}
			s := newSender(t, d)
			d.err = coded{code: tt.code}

			_, err := s.Send(context.Background(), msg())
			if err == nil {
				t.Fatal("Send succeeded")
			}
			if got := shared.IsPermanentSend(err); got != tt.permanent {
				t.Errorf("permanent = %v, want %v (%v)", got, tt.permanent, err)
			}
		})
	}
}

// An unclassified failure counts as transient, as shared.IsPermanentSend says.
func TestAnErrorWithNoCodeIsWorthAnotherTry(t *testing.T) {
	d := &dialer{}
	s := newSender(t, d)

	d.err = errors.New("something nobody classified")
	_, err := s.Send(context.Background(), msg())
	if err == nil {
		t.Fatal("Send succeeded")
	}
	if shared.IsPermanentSend(err) {
		t.Errorf("an unclassified failure was called permanent: %v", err)
	}
}

// Every one of these is a configuration mistake, so it answers as one: the core
// turns it into NO_SENDER, which points the source at their setup.
func TestSettingsThatCannotBeUsed(t *testing.T) {
	cases := map[string]email.Config{
		"no from": {Host: "smtp.acme.test", Port: 587},
		"from is not an address": {
			Host: "smtp.acme.test", Port: 587, From: "not an address",
		},
		"unknown content type": {
			Host: "smtp.acme.test", Port: 587, From: "srosha@acme.test",
			ContentType: "text/markdown",
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := email.New(&dialer{}, cfg, "pw"); !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("New() = %v, want invalid input", err)
			}
		})
	}

	if _, err := email.New(nil, config(), "pw"); err == nil {
		t.Error("New with no dialer succeeded")
	}
}

// A host the dialer will not accept is refused at registration rather than on
// every message.
func TestAnIdentityTheDialerRefuses(t *testing.T) {
	d := &dialer{err: errors.New("smtp: no host")}

	if _, err := email.New(d, config(), "pw"); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("New() = %v, want invalid input", err)
	}
	if d.opened == 0 {
		t.Error("the identity was never checked")
	}
}

func TestParseConfigDefaultsTheContentType(t *testing.T) {
	got, err := email.ParseConfig([]byte(`{"host":"smtp.acme.test","from":"srosha@acme.test"}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got.ContentType != email.TypePlain {
		t.Errorf("ContentType = %q, want plain text", got.ContentType)
	}
	if got.Port != 0 {
		t.Errorf("Port = %d, want it left for infra to default", got.Port)
	}

	if _, err := email.ParseConfig([]byte("host=smtp.acme.test")); !errs.IsType(
		err,
		errs.ErrInvalidInput,
	) {
		t.Errorf("ParseConfig() on rubbish = %v, want invalid input", err)
	}
}

func TestHTMLIsAllowedWhenAskedFor(t *testing.T) {
	got, err := email.ParseConfig(
		[]byte(`{"host":"smtp.acme.test","from":"srosha@acme.test","content_type":"text/html"}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got.ContentType != email.TypeHTML {
		t.Errorf("ContentType = %q, want html", got.ContentType)
	}
}

func TestTheChannelIsEmail(t *testing.T) {
	if got := newSender(t, &dialer{}).Channel(); got != shared.ChannelEmail {
		t.Errorf("Channel() = %q", got)
	}
}
