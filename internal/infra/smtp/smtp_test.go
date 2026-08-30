package smtp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/smtp"
)

func dialer(t *testing.T) *smtp.Dialer {
	t.Helper()

	d, err := smtp.NewDialer(smtp.DialerConfig{
		Timeout:             5 * time.Second,
		TrustAnyCertificate: true,
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	return d
}

func client(t *testing.T, replies map[string]string) (*smtp.Client, *server) {
	t.Helper()

	srv, host, port := newServer(t, replies)

	c, err := dialer(t).Open(smtp.Identity{
		Host: host, Port: port, Username: "acme", Password: "a-password",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c, srv
}

func message() smtp.Message {
	return smtp.Message{
		From: "srosha@acme.test", To: "someone@acme.test",
		Subject: "Deploy", Body: "it is done", ContentType: "text/plain",
	}
}

func TestASentMessageComesBackWithItsMessageID(t *testing.T) {
	c, srv := client(t, nil)

	id, err := c.Send(context.Background(), message())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id == "" {
		t.Error("no Message-ID came back")
	}
	if strings.ContainsAny(id, "<>") {
		t.Errorf("id = %q, want the value without its angle brackets", id)
	}

	var saw bool
	for _, line := range srv.said() {
		if strings.HasPrefix(strings.ToUpper(line), "RCPT TO") &&
			strings.Contains(line, "someone@acme.test") {
			saw = true
		}
	}
	if !saw {
		t.Errorf("the recipient never reached the server: %v", srv.said())
	}
}

// The reason this package uses a library at all: a subject that is not ASCII
// has to be encoded per RFC 2047, and in this service that is the ordinary case.
func TestAPersianSubjectSurvives(t *testing.T) {
	c, _ := client(t, nil)

	m := message()
	m.Subject, m.Body = "استقرار تمام شد", "سلام"

	if _, err := c.Send(context.Background(), m); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

// Encryption is not optional, and the test says so by speaking real STARTTLS.
// A server offering none must be refused rather than talked to in the clear.
func TestAServerThatWillNotEncryptIsRefused(t *testing.T) {
	srv, host, port := newServer(t, nil)
	srv.noTLS = true

	c, err := dialer(t).Open(smtp.Identity{
		Host: host, Port: port, Username: "acme", Password: "a-password",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := c.Send(context.Background(), message()); err == nil {
		t.Fatal("Send succeeded over an unencrypted connection")
	}
}

// The reply code is the one part of an SMTP failure that is not free text, and
// the only thing this package promises about an error.
func TestTheReplyCodeComesBack(t *testing.T) {
	tests := map[string]struct {
		replies map[string]string
		code    int
	}{
		"no such mailbox": {map[string]string{"RCPT TO": "550 5.1.1 No such user here"}, 550},
		"relay denied":    {map[string]string{"RCPT TO": "554 5.7.1 Relay access denied"}, 554},
		"message too big": {map[string]string{"DATA-END": "552 5.3.4 Message too big"}, 552},
		"greylisted":      {map[string]string{"RCPT TO": "450 4.2.0 Greylisted"}, 450},
		"service busy":    {map[string]string{"MAIL FROM": "421 4.3.2 Service not available"}, 421},
		"mailbox full":    {map[string]string{"DATA-END": "452 4.2.2 Mailbox full"}, 452},
		"auth refused": {
			map[string]string{"AUTH": "535 5.7.8 Authentication credentials invalid"},
			535,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := client(t, tt.replies)

			_, err := c.Send(context.Background(), message())
			if err == nil {
				t.Fatal("Send succeeded")
			}

			var coded interface{ ReplyCode() int }
			if !asReplyCoded(err, &coded) {
				t.Fatalf("error carries no reply code: %v", err)
			}
			if got := coded.ReplyCode(); got != tt.code {
				t.Errorf("ReplyCode() = %d, want %d (%v)", got, tt.code, err)
			}
		})
	}
}

// A server that was never reached says nothing about the message, and carries
// no code to say it with.
func TestAServerWeCannotReachCarriesNoCode(t *testing.T) {
	c, err := dialer(t).Open(smtp.Identity{Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err = c.Send(context.Background(), message())
	if err == nil {
		t.Fatal("Send succeeded against nothing")
	}

	var coded interface{ ReplyCode() int }
	if asReplyCoded(err, &coded) && coded.ReplyCode() != 0 {
		t.Errorf("ReplyCode() = %d, want none", coded.ReplyCode())
	}
}

// A relay that authenticates by network rather than by password is a real
// setup, and must not be forced to invent a user.
func TestARelayWithNoAccount(t *testing.T) {
	srv, host, port := newServer(t, nil)

	c, err := dialer(t).Open(smtp.Identity{Host: host, Port: port})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := c.Send(context.Background(), message()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for _, line := range srv.said() {
		if strings.HasPrefix(strings.ToUpper(line), "AUTH") {
			t.Errorf("it authenticated anyway: %v", srv.said())
		}
	}
}

func TestAnIdentityThatCannotBeUsed(t *testing.T) {
	d := dialer(t)

	cases := map[string]smtp.Identity{
		"no host":             {Port: 587},
		"impossible port":     {Host: "smtp.acme.test", Port: 70000},
		"user with no secret": {Host: "smtp.acme.test", Port: 587, Username: "acme"},
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := d.Open(id); err == nil {
				t.Error("Open succeeded")
			}
		})
	}

	// Port zero is the ordinary case, not a mistake: it means submission.
	if _, err := d.Open(smtp.Identity{Host: "smtp.acme.test"}); err != nil {
		t.Errorf("Open with no port = %v, want the submission default", err)
	}
}

func TestADialerNeedsATimeout(t *testing.T) {
	if _, err := smtp.NewDialer(smtp.DialerConfig{}); err == nil {
		t.Error("NewDialer with no timeout succeeded")
	}
}

// asReplyCoded is errors.As for an interface, which needs a pointer to it.
func asReplyCoded(err error, target *interface{ ReplyCode() int }) bool {
	for err != nil {
		if c, ok := err.(interface{ ReplyCode() int }); ok { //nolint:errorlint // walked by hand
			*target = c
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// A message with both halves goes out as ONE mail, not two, and the html part
// comes last -- which is what makes a client prefer it and leaves the plain
// half as the fallback rather than the message.
func TestBothHalvesGoOutAsOneMultipartMessage(t *testing.T) {
	c, srv := client(t, nil)

	m := message()
	m.HTML = "<p>it is done, in markup</p>"

	if _, err := c.Send(context.Background(), m); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := srv.received()
	if !strings.Contains(got, "multipart/alternative") {
		t.Fatalf("the mail is not multipart/alternative:\n%s", got)
	}

	plain := strings.Index(got, "text/plain")
	html := strings.Index(got, "text/html")
	switch {
	case plain < 0:
		t.Error("the plain half never went out")
	case html < 0:
		t.Error("the html half never went out")
	case html < plain:
		t.Error("the html part comes first, so a client will prefer the plain one")
	}
}

// Without an html half nothing changes: the mail stays a single plain part, so
// adding markup to one letter did not quietly re-shape every other message.
func TestAMessageWithNoHTMLHalfStaysPlain(t *testing.T) {
	c, srv := client(t, nil)

	if _, err := c.Send(context.Background(), message()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := srv.received(); strings.Contains(got, "multipart") {
		t.Errorf("a plain message went out as multipart:\n%s", got)
	}
}
