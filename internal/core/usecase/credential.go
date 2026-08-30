package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// CredentialSecrets keeps a sending identity's secret.
//
// Declared here because the core has nowhere to put a secret: credential.
// Credential deliberately holds none, so the entity and its material travel as
// two arguments and are joined by the adapter. Whether that adapter encrypts,
// with which key, and how a key change is survived are questions this layer
// does not ask.
type CredentialSecrets interface {
	Add(ctx context.Context, c *credential.Credential, config []byte, secret string) error

	// Replace writes a new secret over the old one, keeping the identity. It is
	// what a leaked token needs: without it a source would have to register a
	// second identity under a new name, and every message still naming the old
	// one would fail.
	Replace(ctx context.Context, c *credential.Credential, secret string) error
}

// CredentialDefaults moves which identity a channel falls back to.
//
// A second dependency rather than another method above: nothing here is secret,
// and a method that seals nothing has no business in the package that seals.
// ClearDefault is the first half of moving the default and only ever runs next
// to Add, in one transaction -- alone, it leaves the channel with no default at
// all and every message that names no identity fails.
// CredentialSettings replaces the provider settings of an identity.
//
// A third dependency rather than a method on the vault: settings are not secret,
// and nothing about them is sealed. It writes config and nothing else -- the
// name is what a message asks for, so renaming would break every message still
// asking, which is the same reason rotating a secret keeps it.
type CredentialSettings interface {
	UpdateConfig(
		ctx context.Context, sourceID string, id shared.ID, config []byte, now time.Time,
	) error
}

type CredentialDefaults interface {
	ClearDefault(ctx context.Context, sourceID string, c shared.Channel, now time.Time) error
}

// CredentialRegistration is a request to register a sending identity.
//
// Config is whatever the provider needs that is not secret -- an SMTP host, a
// chat id -- and stays raw json all the way down: giving it a shape here would
// make a second provider a change in three layers.
type CredentialRegistration struct {
	Channel   shared.Channel
	Name      string
	Config    []byte
	Secret    string `json:"-"`
	IsDefault bool
}

// String keeps the secret out of whatever this ends up inside. A command struct
// reaches a log line eventually, and this is the one field that must not.
func (r CredentialRegistration) String() string {
	return fmt.Sprintf("CredentialRegistration{Channel:%q, Name:%q, IsDefault:%t}",
		r.Channel, r.Name, r.IsDefault)
}

// Credentials registers the identities a source sends as.
//
// source.Load rather than Admit: registering an identity is not sending, and
// must not cost a message from the sending quota.
type Credentials struct {
	sources  *source.Service
	creds    *credential.Service
	secrets  CredentialSecrets
	defaults CredentialDefaults
	settings CredentialSettings
	uow      UnitOfWork
	newID    shared.IDFunc
	now      shared.NowFunc
}

func NewCredentials(
	sources *source.Service,
	creds *credential.Service,
	secrets CredentialSecrets,
	defaults CredentialDefaults,
	settings CredentialSettings,
	uow UnitOfWork,
	newID shared.IDFunc,
	now shared.NowFunc,
) *Credentials {
	return &Credentials{
		sources: sources, creds: creds, secrets: secrets,
		defaults: defaults, settings: settings,
		uow: uow, newID: newID, now: now,
	}
}

// Register opens a sending identity for one source on one channel.
//
// Taking the default over is two writes and they must be one: the index refuses
// two defaults, so without the first the second fails instead of taking over --
// and with the first alone, the channel is left with no default and every
// message that names no identity fails.
func (c *Credentials) Register(
	ctx context.Context, sourceID string, reg CredentialRegistration,
) (*credential.Credential, error) {
	if _, err := c.sources.Manage(ctx, sourceID); err != nil {
		return nil, err
	}
	if err := validConfig(reg.Config); err != nil {
		return nil, err
	}

	now := c.now()
	cred, err := credential.New(c.newID(), sourceID, reg.Channel, reg.Name, reg.IsDefault, now)
	if err != nil {
		return nil, err
	}

	err = c.uow.Atomically(ctx, func(ctx context.Context) error {
		if reg.IsDefault {
			if err := c.defaults.ClearDefault(ctx, sourceID, reg.Channel, now); err != nil {
				return err
			}
		}
		return c.secrets.Add(ctx, cred, reg.Config, reg.Secret)
	})
	if err != nil {
		return nil, err
	}
	return cred, nil
}

// validConfig refuses a document the database would refuse anyway, so the source
// is told what it did wrong instead of being told the write failed.
func validConfig(config []byte) error {
	if len(config) == 0 || json.Valid(config) {
		return nil
	}
	return errs.InvalidInputErr("credential settings are not valid json").
		WithStr(fmt.Sprintf("%d bytes", len(config)))
}

// List is what a source has registered, on every channel.
//
// Switched-off ones are in it, and that is the point: the answer to "what do I
// have" must include the one somebody disabled, or nobody can turn it back on.
func (c *Credentials) List(ctx context.Context, sourceID string) ([]credential.Credential, error) {
	if _, err := c.sources.Manage(ctx, sourceID); err != nil {
		return nil, err
	}
	return c.creds.List(ctx, sourceID)
}

// Deactivate switches an identity off without forgetting it, so turning it back
// on is not a re-registration. Never deleted: after an incident the first
// question is when it was withdrawn, and a deleted row answers nothing.
//
// If it held the default, the channel is left with none until the source names
// one. Guessing which should take over would move it silently.
func (c *Credentials) Deactivate(
	ctx context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	cred, err := c.get(ctx, sourceID, id)
	if err != nil {
		return nil, err
	}
	if err := c.creds.Deactivate(ctx, cred); err != nil {
		return nil, err
	}
	return cred, nil
}

func (c *Credentials) Activate(
	ctx context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	cred, err := c.get(ctx, sourceID, id)
	if err != nil {
		return nil, err
	}
	if err := c.creds.Activate(ctx, cred); err != nil {
		return nil, err
	}
	return cred, nil
}

// SetDefault moves which identity a message that names none uses.
//
// Two writes and they must be one, for the same reason Register's are: the index
// refuses two defaults on a channel, so without the clear this fails instead of
// taking over -- and with the clear alone the channel is left with none.
func (c *Credentials) SetDefault(
	ctx context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	cred, err := c.get(ctx, sourceID, id)
	if err != nil {
		return nil, err
	}

	err = c.uow.Atomically(ctx, func(ctx context.Context) error {
		if err := c.defaults.ClearDefault(ctx, sourceID, cred.Channel, c.now()); err != nil {
			return err
		}
		return c.creds.MakeDefault(ctx, cred)
	})
	if err != nil {
		return nil, err
	}
	return cred, nil
}

// Rotate replaces the secret and keeps the name, which is what a leaked token
// needs. The alternative -- register a second identity, abandon the first --
// makes every message still naming the old one fail, so a leak on their side
// becomes a code change on their side too.
func (c *Credentials) Rotate(
	ctx context.Context, sourceID string, id shared.ID, secret string,
) (*credential.Credential, error) {
	cred, err := c.get(ctx, sourceID, id)
	if err != nil {
		return nil, err
	}
	if err := c.secrets.Replace(ctx, cred, secret); err != nil {
		return nil, err
	}
	return cred, nil
}

// get is the one lookup all of these share: the source has to exist and be
// active, and the identity has to be that source's own.
func (c *Credentials) get(
	ctx context.Context, sourceID string, id shared.ID,
) (*credential.Credential, error) {
	if _, err := c.sources.Manage(ctx, sourceID); err != nil {
		return nil, err
	}
	if id.IsZero() {
		return nil, errs.InvalidInputErr("credential id is required").WithErr(shared.ErrInvalidID)
	}
	return c.creds.Get(ctx, sourceID, id)
}

// Update replaces an identity's provider settings, keeping its name.
//
// The name stays because a message asks for it: renaming would break every
// message still naming it, which is the same reason Rotate keeps it. The flags
// stay because they have methods of their own.
//
// What arrives is the whole settings document, not a patch. A patch would need
// this layer to know which fields exist, and it deliberately does not -- what a
// provider needs is the provider's business.
func (c *Credentials) Update(
	ctx context.Context, sourceID string, id shared.ID, config []byte,
) (*credential.Credential, error) {
	cred, err := c.get(ctx, sourceID, id)
	if err != nil {
		return nil, err
	}
	if err := validConfig(config); err != nil {
		return nil, err
	}
	if err := c.settings.UpdateConfig(ctx, sourceID, id, config, c.now()); err != nil {
		return nil, err
	}
	return cred, nil
}
