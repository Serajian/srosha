// Package smtp hands a message to a mail server and owns how that conversation
// happens: which port means which encryption, which authentication is offered,
// and how long any of it may take. It knows nothing about what it is carrying.
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wneessen/go-mail"
)

// implicitTLSPort is TLS from the first byte rather than after STARTTLS.
// Recognized by number because that is how it is configured everywhere: a mail
// provider says "465" and means "wrapped", never "587 but encrypted".
const implicitTLSPort = 465

// defaultPort is submission with STARTTLS, which is what a provider hands a
// customer unless they ask for something else.
const defaultPort = 587

// DialerConfig is what every identity shares. It is not the service's settings
// type: infra should stay copyable into another service, and mapping one to the
// other is registry's job.
type DialerConfig struct {
	// Timeout bounds the whole conversation -- connect, greet, authenticate,
	// hand the message over. SMTP has no shorter unit to bound: a server that
	// stops answering mid-transaction leaves a read waiting with nothing of its
	// own to expire.
	Timeout time.Duration

	// TrustAnyCertificate turns off certificate verification. It exists for a
	// test speaking real TLS to a certificate it made a moment ago, and for
	// nothing else -- encryption itself is never optional here.
	TrustAnyCertificate bool
}

func (c DialerConfig) validate() error {
	if c.Timeout <= 0 {
		return errors.New("smtp: timeout must be above zero")
	}
	return nil
}

// Identity is one mail account: where to hand a message over and as whom.
type Identity struct {
	Host string

	// Port picks the encryption too. 465 is TLS from the first byte, anything
	// else is STARTTLS -- required rather than attempted, because a server that
	// will not encrypt is a server this password does not go to.
	Port int

	// Username may be empty, for a relay that authenticates by network rather
	// than by password.
	Username string

	// Never marshaled: an identity reaches a log line eventually.
	Password string `json:"-"`
}

// String keeps the password out of whatever this ends up inside.
func (i Identity) String() string {
	return fmt.Sprintf("Identity{Host:%q, Port:%d, Username:%q}", i.Host, i.Port, i.Username)
}

func (i Identity) validate() error {
	if strings.TrimSpace(i.Host) == "" {
		return errors.New("smtp: no host")
	}
	if i.Port < 1 || i.Port > 65535 {
		return fmt.Errorf("smtp: port %d is not usable", i.Port)
	}
	if i.Username != "" && i.Password == "" {
		return errors.New("smtp: a user with no password")
	}
	return nil
}

// Message is what goes out. Addresses and text, nothing about delivery.
type Message struct {
	From    string
	To      string
	Subject string

	Body string

	// ContentType is the body's, "text/plain" or "text/html".
	ContentType string
}

// Dialer opens a client per identity, and holds only what they share.
//
// A client per identity rather than one shared, because mail is not http: every
// source may send through its own server as its own account, so there is nothing
// for a single connection to be shared between.
type Dialer struct{ cfg DialerConfig }

func NewDialer(cfg DialerConfig) (*Dialer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Dialer{cfg: cfg}, nil
}

// Open prepares a client for one identity. It connects to nothing: SMTP has no
// connection worth keeping between messages -- a server closes an idle session
// on its own schedule -- so Send dials, hands over and hangs up.
func (d *Dialer) Open(id Identity) (*Client, error) {
	if id.Port == 0 {
		id.Port = defaultPort
	}
	if err := id.validate(); err != nil {
		return nil, err
	}

	opts := []mail.Option{
		mail.WithPort(id.Port),
		mail.WithTimeout(d.cfg.Timeout),
		mail.WithTLSPolicy(mail.TLSMandatory),
	}
	if id.Port == implicitTLSPort {
		opts = append(opts, mail.WithSSL())
	}
	if d.cfg.TrustAnyCertificate {
		opts = append(opts, mail.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // see DialerConfig.TrustAnyCertificate
			ServerName:         id.Host,
		}))
	}

	if id.Username == "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthNoAuth))
	} else {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(id.Username),
			mail.WithPassword(id.Password),
		)
	}

	client, err := mail.NewClient(id.Host, opts...)
	if err != nil {
		return nil, wrap("open", err)
	}
	return &Client{client: client}, nil
}

// Client is one identity's way to a mail server.
type Client struct{ client *mail.Client }

// Send hands one message over and returns the Message-ID it went out with.
//
// SMTP gives back nothing dependable to identify a message: some servers put a
// queue id in the 250 line and some do not, and its shape is nobody's standard.
// The Message-ID is the handle that exists on both sides, because it is a header
// of the mail itself.
func (c *Client) Send(ctx context.Context, m Message) (string, error) {
	msg := mail.NewMsg()

	if err := msg.From(m.From); err != nil {
		return "", wrap("from", err)
	}
	if err := msg.To(m.To); err != nil {
		// The address is somebody's, so it is not quoted back into an error.
		return "", wrap("to", errors.New("the recipient address cannot be used"))
	}

	// Through the library rather than written into a header by hand: a subject
	// that is not ASCII has to be encoded per RFC 2047, and getting that wrong
	// is a garbled subject on every message rather than a failure anybody sees.
	msg.Subject(m.Subject)
	msg.SetBodyString(mail.ContentType(m.ContentType), m.Body)
	msg.SetMessageID()

	if err := c.client.DialAndSendWithContext(ctx, msg); err != nil {
		return "", wrap("send", err)
	}

	ids := msg.GetGenHeader(mail.HeaderMessageID)
	if len(ids) == 0 {
		return "", wrap("send", errors.New("the message went out with no Message-ID"))
	}
	return strings.Trim(ids[0], "<>"), nil
}
