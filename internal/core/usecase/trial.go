package usecase

import (
	"context"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// TrialLimiter bounds how often one source may send a test.
//
// Declared here rather than reusing source.RateLimiter: that one is the sending
// quota, spent by Admit in another process, and a trial must not look like it
// costs a message. This layer only needs to be told yes or no.
type TrialLimiter interface {
	Allow(ctx context.Context, sourceID string) (bool, error)
}

// Trials sends one real message through an identity a source registered, so a
// customer finds out whether it works now rather than when a notification does
// not arrive.
//
// A type of its own rather than a method on Credentials: two binaries build
// that one -- the console and the gateway -- and the gateway has no sender
// adapters at all. Requiring a registry there would be asking it for something
// it structurally does not have.
type Trials struct {
	sources *source.Service
	creds   *credential.Service
	senders delivery.SenderRegistry
	gate    *Gate
	limiter TrialLimiter
	newID   shared.IDFunc
}

func NewTrials(
	sources *source.Service, creds *credential.Service, senders delivery.SenderRegistry,
	gate *Gate, limiter TrialLimiter, newID shared.IDFunc,
) *Trials {
	return &Trials{
		sources: sources, creds: creds, senders: senders,
		gate: gate, limiter: limiter, newID: newID,
	}
}

// Run sends the test and hands back the provider's own id for it.
//
// The order of the refusals is deliberate: nothing is spent before the source
// is known to be allowed, and the cap is taken only once there is something
// real to send.
func (t *Trials) Run(
	ctx context.Context, actor *user.User, sourceID string, credentialID shared.ID,
) (string, error) {
	// Load rather than Manage: a source waiting for review has not been let
	// out, and a trial would be srosha sending for something it has not
	// approved. Load refuses an inactive one on its own.
	src, err := t.sources.Load(ctx, sourceID)
	if err != nil {
		return "", err
	}

	// Scoped by source, so a guessed id finds nothing rather than finding
	// somebody else's identity. That is a 404 and not a 403 on purpose:
	// whether an id exists is not a customer's business.
	cred, err := t.creds.Get(ctx, sourceID, credentialID)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", errs.NotFoundErr("no credential with that id").
			WithErr(credential.ErrNotFound).
			WithStr(credentialID.String())
	}

	address := src.DefaultAddresses[cred.Channel]
	if address == "" {
		return "", errs.InvalidInputErr(
			"this channel has no default address, so a test has nowhere to go").
			WithErr(ErrTrialNoAddress).
			WithStr(cred.Channel.String())
	}

	allowed, err := t.limiter.Allow(ctx, sourceID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errs.TooManyErr("too many tests just now").
			WithErr(ErrTrialTooMany).
			WithStr(cred.Channel.String())
	}

	// By NAME, exactly as a real send resolves. A trial that resolved
	// differently would prove something other than what was asked -- and an
	// identity the source switched off has to be refused here for the same
	// reason it is refused in production.
	out, err := t.senders.For(ctx, sourceID, cred.Channel, cred.Name)
	if err != nil {
		return "", err
	}

	message := shared.Message{
		DeliveryID: t.newID(),
		Recipient:  shared.Recipient{Channel: cred.Channel, Address: address},
		Title:      trialTitle,
		Body:       trialBody,
	}
	act := Act{
		Verb:       ActCredentialTest,
		TargetType: "credential",
		TargetID:   credentialID.String(),
		Note:       cred.Name,
	}

	var providerID string
	err = t.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		providerID, err = out.Send(ctx, message)
		return err
	})
	if err != nil {
		return "", err
	}
	return providerID, nil
}
