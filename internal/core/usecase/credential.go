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
}

// CredentialDefaults moves which identity a channel falls back to.
//
// A second dependency rather than another method above: nothing here is secret,
// and a method that seals nothing has no business in the package that seals.
// ClearDefault is the first half of moving the default and only ever runs next
// to Add, in one transaction -- alone, it leaves the channel with no default at
// all and every message that names no identity fails.
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
	secrets  CredentialSecrets
	defaults CredentialDefaults
	uow      UnitOfWork
	newID    shared.IDFunc
	now      shared.NowFunc
}

func NewCredentials(
	sources *source.Service,
	secrets CredentialSecrets,
	defaults CredentialDefaults,
	uow UnitOfWork,
	newID shared.IDFunc,
	now shared.NowFunc,
) *Credentials {
	return &Credentials{
		sources: sources, secrets: secrets, defaults: defaults,
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
	if _, err := c.sources.Load(ctx, sourceID); err != nil {
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
