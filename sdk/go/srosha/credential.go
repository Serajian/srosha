package srosha

import (
	"context"
	"time"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
)

// Credentials is a source's sending identities.
//
// A source registers each one once, and then never mentions it again: Submit
// names a channel, not an identity. Only a channel with more than one needs
// Route.From to say which.
type Credentials struct {
	client *Client
	api    pb.CredentialServiceClient
}

// Identity is a sending identity as the service describes it back.
//
// It has no secret field and never will. Everything here is safe to log, to
// cache and to show in a console; the secret went one way.
type Identity struct {
	ID      string
	Channel Channel

	// Name is how a message asks for it later, in Route.From.
	Name string

	// Default says messages naming no identity on this channel use this one.
	Default bool

	// Active says it may be used. A switched-off identity is still listed --
	// without that, nobody could turn one back on.
	Active bool

	CreatedAt time.Time
}

// Registration is what opening an identity needs.
type Registration struct {
	// Name is what this source calls it: "marketing", "alerts". Lowercase
	// letters, digits and hyphens, because it travels in a url and a config key.
	Name string

	// Default makes messages that name no identity on this channel use it. A
	// channel has at most one, so registering a second default moves it.
	Default bool

	// Credential says which channel this is for, as well as how to send on it.
	Credential Credential
}

// Register opens an identity on one channel.
func (c *Credentials) Register(ctx context.Context, r Registration) (Identity, error) {
	if r.Credential == nil {
		return Identity{}, &Error{
			kind:    ErrInvalidRequest,
			Message: "a registration needs a credential",
		}
	}

	settings, err := r.Credential.Settings()
	if err != nil {
		return Identity{}, &Error{kind: ErrInvalidRequest, Message: err.Error()}
	}

	req := &pb.CredentialServiceRegisterRequest{
		Channel:   toChannel(r.Credential.channel()),
		Name:      r.Name,
		Config:    settings,
		Secret:    r.Credential.secret(),
		IsDefault: r.Default,
	}

	var res *pb.CredentialServiceRegisterResponse
	if err := c.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.api.Register(ctx, req)
		return err
	}); err != nil {
		return Identity{}, err
	}
	return fromIdentity(res.GetCredential()), nil
}

// List answers "what have I registered". Pass an empty channel for all of them.
//
// Switched-off identities are in it, and that is the point: without them
// nobody could turn one back on.
func (c *Credentials) List(ctx context.Context, channel Channel) ([]Identity, error) {
	var res *pb.CredentialServiceListResponse
	if err := c.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.api.List(ctx, &pb.CredentialServiceListRequest{
			Channel: toChannel(channel),
		})
		return err
	}); err != nil {
		return nil, err
	}

	out := make([]Identity, 0, len(res.GetCredentials()))
	for _, cr := range res.GetCredentials() {
		out = append(out, fromIdentity(cr))
	}
	return out, nil
}

// Rotate replaces the secret and keeps the name, which is what a leaked token
// needs: registering a second identity instead would make every message still
// naming the old one fail, turning a leak into a code change.
//
// The secret alone, because that is the only half that changes. Use Update for
// the settings.
func (c *Credentials) Rotate(ctx context.Context, id, secret string) (Identity, error) {
	var res *pb.CredentialServiceRotateResponse
	if err := c.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.api.Rotate(ctx, &pb.CredentialServiceRotateRequest{
			Id: id, Secret: secret,
		})
		return err
	}); err != nil {
		return Identity{}, err
	}
	return fromIdentity(res.GetCredential()), nil
}

// Update replaces the provider settings and keeps the name and the secret.
// Changing a mail server is what this is for.
//
// **Only the settings are sent.** A secret set on the credential passed here is
// ignored -- use Rotate for that. The whole settings document goes, not a
// patch: srosha does not know which fields a provider has, and a patch would
// mean it had to.
func (c *Credentials) Update(ctx context.Context, id string, cred Credential) (Identity, error) {
	if cred == nil {
		return Identity{}, &Error{
			kind:    ErrInvalidRequest,
			Message: "an update needs a credential to read settings from",
		}
	}

	settings, err := cred.Settings()
	if err != nil {
		return Identity{}, &Error{kind: ErrInvalidRequest, Message: err.Error()}
	}

	var res *pb.CredentialServiceUpdateResponse
	if err := c.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.api.Update(ctx, &pb.CredentialServiceUpdateRequest{
			Id: id, Config: settings,
		})
		return err
	}); err != nil {
		return Identity{}, err
	}
	return fromIdentity(res.GetCredential()), nil
}

// Deactivate stops an identity being used without forgetting it, so turning it
// back on is not a re-registration. Nothing here deletes: after an incident the
// first question is when an identity was withdrawn, and a deleted row answers
// nothing.
//
// If it held the default, the channel is left with none until SetDefault names
// one. Guessing which should take over would move it silently.
func (c *Credentials) Deactivate(ctx context.Context, id string) (Identity, error) {
	var res *pb.CredentialServiceDeactivateResponse
	if err := c.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.api.Deactivate(ctx, &pb.CredentialServiceDeactivateRequest{Id: id})
		return err
	}); err != nil {
		return Identity{}, err
	}
	return fromIdentity(res.GetCredential()), nil
}

// Activate is the way back. It does not hand the default back.
func (c *Credentials) Activate(ctx context.Context, id string) (Identity, error) {
	var res *pb.CredentialServiceActivateResponse
	if err := c.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.api.Activate(ctx, &pb.CredentialServiceActivateRequest{Id: id})
		return err
	}); err != nil {
		return Identity{}, err
	}
	return fromIdentity(res.GetCredential()), nil
}

// SetDefault moves which identity a message that names none uses. A channel has
// at most one, so this takes it from whichever held it.
func (c *Credentials) SetDefault(ctx context.Context, id string) (Identity, error) {
	var res *pb.CredentialServiceSetDefaultResponse
	if err := c.client.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.api.SetDefault(ctx, &pb.CredentialServiceSetDefaultRequest{Id: id})
		return err
	}); err != nil {
		return Identity{}, err
	}
	return fromIdentity(res.GetCredential()), nil
}

func fromIdentity(c *pb.Credential) Identity {
	if c == nil {
		return Identity{}
	}
	return Identity{
		ID:        c.GetId(),
		Channel:   fromChannel(c.GetChannel()),
		Name:      c.GetName(),
		Default:   c.GetIsDefault(),
		Active:    c.GetIsActive(),
		CreatedAt: fromTimestamp(c.GetCreatedAt()),
	}
}
