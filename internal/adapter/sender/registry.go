// Package sender decides which identity a message goes out as, and builds the
// thing that sends it. The providers themselves are its subpackages; this is
// only the choosing.
package sender

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Serajian/srosha/internal/adapter/sender/apns"
	"github.com/Serajian/srosha/internal/adapter/sender/bale"
	"github.com/Serajian/srosha/internal/adapter/sender/email"
	"github.com/Serajian/srosha/internal/adapter/sender/fcm"
	"github.com/Serajian/srosha/internal/adapter/sender/gotify"
	"github.com/Serajian/srosha/internal/adapter/sender/matrix"
	"github.com/Serajian/srosha/internal/adapter/sender/telegram"
	"github.com/Serajian/srosha/internal/adapter/sender/whatsapp"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/infra/appleauth"
	"github.com/Serajian/srosha/internal/infra/googleauth"
	"github.com/Serajian/srosha/pkg/errs"
)

// GoogleTokens turns a service account into a supply of access tokens.
//
// Declared here rather than imported as a concrete type, because one adapter
// never reaches into another. internal/infra/googleauth satisfies it, and the
// caching that makes it worth having belongs to whoever opened it.
//
// It is here rather than in the fcm package because opening a credential is
// this package's job: fcm is handed the result and never sees a private key.
type GoogleTokens interface {
	Open(serviceAccount []byte) (*googleauth.Source, error)
}

// AppleTokens signs the provider tokens APNs authenticates with.
//
// Declared here for the same reason as GoogleTokens, and kept separate because
// they share nothing: Google exchanges a key for a token over the network,
// Apple's is signed locally. What they do have in common is that the result has
// to be kept between messages, and that belongs to whoever opened it.
type AppleTokens interface {
	Open(p8 []byte, id appleauth.Identity) (*appleauth.Source, error)
}

// Secrets opens a credential's material at the moment it is used.
//
// Declared here rather than imported, because one adapter never reaches into
// another. Whoever holds the keys satisfies it; this package never learns that
// anything was encrypted.
type Secrets interface {
	Material(
		ctx context.Context, sourceID string, c shared.Channel, id shared.ID,
	) (config []byte, secret string, err error)
}

// Fallback is srosha's own identity per channel, used when a source has
// registered none of its own.
//
// A channel missing from it is not a startup failure: a deployment that only
// sends on Telegram should not have to invent an SMTP host. The delivery that
// asked for the missing channel fails as NO_SENDER and the source is told,
// rather than the whole service staying down.
//
// It grows one field per channel as each sender is written.
type Fallback struct {
	TelegramToken string
	BaleToken     string

	// WhatsApp is a token and the number it sends from, because Meta identifies
	// the sending number separately from the account that owns it.
	WhatsApp WhatsApp

	// Matrix is a token and the homeserver it belongs to. The homeserver is the
	// one address in this service that is not a constant somewhere: the protocol
	// is federated, so there is no host that is right for everybody.
	Matrix Matrix

	// Gotify is an application token and the self-hosted server it belongs to.
	// The server is the one address in this service that is not a constant
	// somewhere: Gotify is self-hosted, so there is no host that is right for
	// everybody.
	Gotify Gotify

	// FCMServiceAccount is a private key rather than a token, because Google
	// does not hand out tokens: a service account is exchanged for one, and it
	// carries the project it belongs to inside it.
	FCMServiceAccount string

	// APNs is a signing key and the three names that go with it: which key,
	// which developer account, which app. The most fields any channel needs,
	// and none of the three beside the key is secret.
	APNs APNs

	// SMTP is a whole identity rather than a token, because mail is. A bot is a
	// secret and nothing else; a mail account is a server, a user and an address,
	// and any one of them wrong is a message that never arrives.
	SMTP SMTP
}

// WhatsApp is srosha's own business number.
type WhatsApp struct {
	Token string

	// PhoneNumberID is Meta's id for the sending number, not the number itself.
	PhoneNumberID string
}

func (w WhatsApp) configured() bool { return w.Token != "" && w.PhoneNumberID != "" }

// Matrix is srosha's own account on a homeserver.
type Matrix struct {
	Token      string
	Homeserver string
}

func (m Matrix) configured() bool { return m.Token != "" && m.Homeserver != "" }

// Gotify is srosha's own application on a self-hosted server.
type Gotify struct {
	Token     string
	ServerURL string
}

func (g Gotify) configured() bool { return g.Token != "" && g.ServerURL != "" }

// APNs is srosha's own Apple push identity.
type APNs struct {
	// Key is the p8 file's contents, already decoded from the base64 that
	// carried it through the environment.
	Key string

	KeyID       string
	TeamID      string
	Topic       string
	Environment string
}

// String keeps the key out of whatever this ends up inside.
func (a APNs) String() string {
	return fmt.Sprintf("APNs{KeyID:%q, TeamID:%q, Topic:%q, Environment:%q}",
		a.KeyID, a.TeamID, a.Topic, a.Environment)
}

func (a APNs) configured() bool {
	return a.Key != "" && a.KeyID != "" && a.TeamID != "" && a.Topic != ""
}

// SMTP is srosha's own mail identity.
type SMTP struct {
	Host     string
	Port     int
	Username string
	From     string

	// Never marshaled: this struct reaches a log line eventually.
	Password string `json:"-"`
}

// String keeps the password out of whatever this ends up inside.
func (s SMTP) String() string {
	return fmt.Sprintf("SMTP{Host:%q, Port:%d, Username:%q, From:%q}",
		s.Host, s.Port, s.Username, s.From)
}

// configured reports whether there is enough here to send at all. A deployment
// that only sends on Telegram should not have to invent a mail server.
func (s SMTP) configured() bool { return s.Host != "" && s.From != "" }

// Registry implements delivery.SenderRegistry.
type Registry struct {
	creds   *credential.Service
	secrets Secrets
	client  *http.Client

	// mail is what registry opened for SMTP. A dialer rather than a client,
	// because every source may send through its own server as its own account.
	mail email.Dialer

	// tokens is what registry opened for Google. It caches, which is the point:
	// a sender is built per message and minting a token each time would put an
	// RSA signature and a round trip in front of every push.
	tokens GoogleTokens

	// apple is the same thing for APNs. Two of them rather than one interface,
	// because a service account and a p8 key have nothing in common but the
	// word credential.
	apple AppleTokens

	own Fallback
}

func NewRegistry(
	creds *credential.Service, secrets Secrets,
	client *http.Client, mail email.Dialer,
	tokens GoogleTokens, apple AppleTokens, own Fallback,
) (*Registry, error) {
	switch {
	case creds == nil:
		return nil, errs.InternalErr("sender registry cannot resolve credentials")
	case secrets == nil:
		return nil, errs.InternalErr("sender registry cannot open credentials")
	case client == nil:
		return nil, errs.InternalErr("sender registry has no http client")
	case mail == nil:
		return nil, errs.InternalErr("sender registry has no mail dialer")
	case tokens == nil:
		return nil, errs.InternalErr("sender registry cannot mint google tokens")
	case apple == nil:
		return nil, errs.InternalErr("sender registry cannot sign apple tokens")
	}
	return &Registry{
		creds: creds, secrets: secrets, client: client,
		mail: mail, tokens: tokens, apple: apple, own: own,
	}, nil
}

// For hands back a sender already configured with the right identity.
//
// Three answers, and they are not the same:
//
//	the source named one, or has a default   -> theirs, opened here
//	the source registered nothing at all     -> ours
//	the source registered something unusable -> refused
//
// The last two are the distinction that matters. Asking for a named identity
// never reaches the fallback: a message that said "send as our bot" going out
// as srosha's bot is worse than a message that failed, because nobody finds out.
// The same holds for an identity the source switched off -- switching it off was
// a decision, and standing in for it would undo it silently.
func (r *Registry) For(
	ctx context.Context, sourceID string, c shared.Channel, name string,
) (delivery.Sender, error) {
	cred, err := r.creds.Resolve(ctx, sourceID, c, name)
	if err != nil {
		if name == "" && errors.Is(err, credential.ErrNoCredentials) {
			return r.ours(c)
		}
		return nil, err
	}

	config, secret, err := r.secrets.Material(ctx, sourceID, c, cred.ID)
	if err != nil {
		return nil, err
	}
	return r.build(c, config, secret)
}

// ours is the service's own identity. It carries no settings: those belong to a
// source that configured something, and this is the path where none did.
func (r *Registry) ours(c shared.Channel) (delivery.Sender, error) {
	switch c {
	case shared.ChannelTelegram:
		return r.buildOwn(c, r.own.TelegramToken)

	case shared.ChannelBale:
		return r.buildOwn(c, r.own.BaleToken)

	case shared.ChannelEmail:
		if !r.own.SMTP.configured() {
			return nil, noSender(c)
		}
		return email.New(r.mail, email.Config{
			Host:     r.own.SMTP.Host,
			Port:     r.own.SMTP.Port,
			Username: r.own.SMTP.Username,
			From:     r.own.SMTP.From,
		}, r.own.SMTP.Password)

	case shared.ChannelMatrix:
		if !r.own.Matrix.configured() {
			return nil, noSender(c)
		}
		return matrix.New(r.client, r.own.Matrix.Token,
			matrix.Config{Homeserver: r.own.Matrix.Homeserver})

	case shared.ChannelGotify:
		if !r.own.Gotify.configured() {
			return nil, noSender(c)
		}
		return gotify.New(r.client, r.own.Gotify.Token,
			gotify.Config{ServerURL: r.own.Gotify.ServerURL})

	case shared.ChannelWhatsApp:
		if !r.own.WhatsApp.configured() {
			return nil, noSender(c)
		}
		return whatsapp.New(r.client, r.own.WhatsApp.Token,
			whatsapp.Config{PhoneNumberID: r.own.WhatsApp.PhoneNumberID})

	case shared.ChannelFCM:
		if r.own.FCMServiceAccount == "" {
			return nil, noSender(c)
		}
		return r.buildFCM(r.own.FCMServiceAccount)

	case shared.ChannelAPNs:
		if !r.own.APNs.configured() {
			return nil, noSender(c)
		}
		return r.buildAPNs(apns.Config{
			KeyID:       r.own.APNs.KeyID,
			TeamID:      r.own.APNs.TeamID,
			Topic:       r.own.APNs.Topic,
			Environment: r.own.APNs.Environment,
		}, r.own.APNs.Key)

	default:
		return nil, noSender(c)
	}
}

// buildOwn refuses an empty token here rather than letting the provider package
// answer, so "we have no identity on this channel" and "the identity we have is
// not usable" stay one sentence to whoever reads the delivery.
func (r *Registry) buildOwn(c shared.Channel, token string) (delivery.Sender, error) {
	if token == "" {
		return nil, noSender(c)
	}
	return r.build(c, nil, token)
}

// build is the one place a channel becomes a provider. A channel with no case
// here answers as a configuration problem rather than as a fault: the core turns
// it into NO_SENDER, which is reported to the source and not retried.
func (r *Registry) build(c shared.Channel, config []byte, secret string) (delivery.Sender, error) {
	switch c {
	case shared.ChannelTelegram:
		return telegram.New(r.client, secret, config)

	case shared.ChannelBale:
		return bale.New(r.client, secret, config)

	case shared.ChannelEmail:
		// Mail parses its settings into a type of its own rather than taking
		// raw json: they are required and interdependent, so they are checked
		// once here instead of at the moment a message is going out.
		cfg, err := email.ParseConfig(config)
		if err != nil {
			return nil, err
		}
		return email.New(r.mail, cfg, secret)

	case shared.ChannelMatrix:
		// Parsed into a type of its own, as mail and whatsapp are: the
		// homeserver is required, and it is an address somebody else chose.
		cfg, err := matrix.ParseConfig(config)
		if err != nil {
			return nil, err
		}
		return matrix.New(r.client, secret, cfg)

	case shared.ChannelGotify:
		// Parsed into a type of its own, as mail and whatsapp are: the server
		// url is required, and it is an address somebody else chose.
		cfg, err := gotify.ParseConfig(config)
		if err != nil {
			return nil, err
		}
		return gotify.New(r.client, secret, cfg)

	case shared.ChannelWhatsApp:
		// Parsed into a type of its own, as mail is: these settings are required
		// and interdependent, so they are checked once here rather than at the
		// moment a message is going out.
		cfg, err := whatsapp.ParseConfig(config)
		if err != nil {
			return nil, err
		}
		return whatsapp.New(r.client, secret, cfg)

	case shared.ChannelFCM:
		// No settings at all, and config is ignored on purpose: the whole
		// service account is the secret, and the project is inside it.
		return r.buildFCM(secret)

	case shared.ChannelAPNs:
		// The most settings of any channel, and the only one that needs all of
		// them: a key id, a team, an app and an environment. None is secret --
		// the p8 key is, and that is what arrives as the secret.
		cfg, err := apns.ParseConfig(config)
		if err != nil {
			return nil, err
		}
		return r.buildAPNs(cfg, secret)

	default:
		return nil, noSender(c)
	}
}

// buildFCM opens the service account here rather than in the sender, so that a
// private key never reaches a provider package. What fcm gets is a project name
// and something that answers with a token.
func (r *Registry) buildFCM(serviceAccount string) (delivery.Sender, error) {
	if strings.TrimSpace(serviceAccount) == "" {
		return nil, errs.InvalidInputErr("no fcm service account for this identity")
	}

	source, err := r.tokens.Open([]byte(serviceAccount))
	if err != nil {
		// googleauth says what is wrong with the file and quotes nothing from it.
		return nil, errs.InvalidInputErr("fcm service account is not usable").WithErr(err)
	}
	return fcm.New(r.client, source, source.Account().ProjectID)
}

// buildAPNs opens the signing key here rather than in the sender, so that a
// private key never reaches a provider package -- the same split as fcm.
func (r *Registry) buildAPNs(cfg apns.Config, key string) (delivery.Sender, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errs.InvalidInputErr("no apns signing key for this identity")
	}

	source, err := r.apple.Open([]byte(key), appleauth.Identity{
		KeyID: cfg.KeyID, TeamID: cfg.TeamID,
	})
	if err != nil {
		// appleauth says what is wrong with the file and quotes nothing from it.
		return nil, errs.InvalidInputErr("apns signing key is not usable").WithErr(err)
	}
	return apns.New(r.client, source, cfg)
}

func noSender(c shared.Channel) error {
	return errs.InvalidInputErr("no sender configured for this channel").
		WithStr("channel " + c.String())
}
