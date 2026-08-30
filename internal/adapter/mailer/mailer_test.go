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

// letter renders both halves the way SendCode does, so a test asserts on what
// a person actually receives rather than on the half that is easier to build.
func letter(t *testing.T, code string) smtp.Message {
	t.Helper()

	set, err := newLetters(letterSignInCode)
	if err != nil {
		t.Fatalf("newLetters: %v", err)
	}
	html, err := set.render(letterSignInCode, signInCode{Code: code})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return compose(from, "ops@acme.test", code, html)
}

func TestTheCodeIsInTheMessage(t *testing.T) {
	msg := letter(t, "482913")

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
	body := letter(t, "482913").Body

	if !strings.Contains(body, "didn't ask") {
		t.Errorf("body does not reassure an unexpected recipient:\n%s", body)
	}
}

// The code must not turn up in the subject, where it shows in a notification
// on a locked screen.
func TestTheCodeIsNotInTheSubject(t *testing.T) {
	msg := letter(t, "482913")

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

// Which half a person reads is their client's decision, so neither may be the
// only one that carries the code.
func TestBothHalvesCarryTheCode(t *testing.T) {
	msg := letter(t, "482913")

	if !strings.Contains(msg.Body, "482913") {
		t.Error("the plain half has no code in it")
	}
	if !strings.Contains(msg.HTML, "482913") {
		t.Error("the html half has no code in it")
	}
}

// The two halves are one message. This locks the line that would drift first --
// the rules a person needs before they start typing.
func TestBothHalvesSayTheSameThingAboutTheRules(t *testing.T) {
	msg := letter(t, "482913")

	const rules = "Ten minutes. One use. Three guesses."
	if !strings.Contains(msg.Body, rules) {
		t.Errorf("the plain half does not carry %q", rules)
	}
	if !strings.Contains(msg.HTML, rules) {
		t.Errorf("the html half does not carry %q", rules)
	}
}

// A client fills the inbox preview from the top of the body, and that preview
// shows on a locked screen -- the same place the subject shows, for the same
// reason the code is kept out of it. The hidden preview line exists to get
// there first.
func TestTheCodeIsNotWhatTheInboxPreviewFindsFirst(t *testing.T) {
	html := letter(t, "482913").HTML

	preview := strings.Index(html, "Your code is inside this message")
	if preview < 0 {
		t.Fatal("there is no preview line, so the inbox will use the code")
	}
	if code := strings.Index(html, "482913"); code < preview {
		t.Error("the code comes before the preview line and will be previewed")
	}
}

// A letter that will not parse has to stop the binary, not the first person who
// asks to sign in.
func TestALetterThatIsNotThereIsRefusedAtStartup(t *testing.T) {
	if _, err := newLetters("no_such_letter"); err == nil {
		t.Fatal("newLetters accepted a letter that does not exist")
	}
}
