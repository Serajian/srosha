package secret

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/crypto"
	"github.com/Serajian/srosha/pkg/errs"
)

// WebhookStore is where a callback's sealed signing secret is kept.
//
// Declared here rather than imported, for the same reason Store is: the
// postgres repository satisfies it without learning that anything is
// encrypted.
type WebhookStore interface {
	// WriteSecret replaces the sealed value on one source's webhook.
	WriteSecret(
		ctx context.Context, sourceID string, id shared.ID, secret string, now time.Time,
	) error

	// ReadSecret hands back the sealed value, empty if the row has none.
	ReadSecret(ctx context.Context, sourceID string) (id shared.ID, secret string, err error)
}

// WebhookKeeper seals a callback's signing secret and hands it back only when a
// callback is being signed.
//
// A type of its own rather than more methods on Keeper. That one is a sending
// identity's secret all the way down -- its Store speaks in credentials, and
// its reseal-on-read exists because a credential is read on every message. A
// signing secret is read once per callback and belongs to a different entity;
// sharing the type would mean one Store interface answering for two things that
// have nothing in common but being encrypted.
type WebhookKeeper struct {
	store WebhookStore
	keys  *crypto.Keyring
	now   shared.NowFunc
}

func NewWebhookKeeper(
	store WebhookStore, keys *crypto.Keyring, now shared.NowFunc,
) (*WebhookKeeper, error) {
	switch {
	case store == nil:
		return nil, errs.InternalErr("webhook keeper has nowhere to keep secrets")
	case keys == nil:
		return nil, errs.InternalErr("webhook keeper has no keyring")
	case now == nil:
		return nil, errs.InternalErr("webhook keeper has no clock")
	}
	return &WebhookKeeper{store: store, keys: keys, now: now}, nil
}

// Issue makes a secret, seals it onto the webhook, and returns the plaintext.
//
// The plaintext is returned exactly once and never again: what is stored is
// sealed, and nothing reads it back except the signing path. A source that
// loses it rotates rather than recovers -- the same bargain an api key makes.
func (k *WebhookKeeper) Issue(
	ctx context.Context, sourceID string, id shared.ID,
) (string, error) {
	plain, err := newSigningSecret()
	if err != nil {
		return "", err
	}

	sealed, err := k.keys.Seal([]byte(plain), bindWebhook(sourceID, id))
	if err != nil {
		return "", errs.InternalErr("the signing secret could not be stored").WithErr(err)
	}
	if err := k.store.WriteSecret(ctx, sourceID, id, sealed, k.now()); err != nil {
		return "", err
	}
	return plain, nil
}

// SecretFor opens the secret for one source's callback.
//
// It satisfies what the notifier declared, which is why it takes a source id
// and nothing else: the notifier knows whose callback it is about and must not
// have to know which webhook row that is.
func (k *WebhookKeeper) SecretFor(sourceID string) (string, bool) {
	// The notifier's port has no context, because it is called from inside a
	// send that already has one. Using Background here is deliberate rather
	// than careless: the read is a single indexed row and the send around it
	// carries the deadline that matters.
	id, sealed, err := k.store.ReadSecret(context.Background(), sourceID)
	if err != nil || sealed == "" {
		return "", false
	}

	plain, err := k.keys.Open(sealed, bindWebhook(sourceID, id))
	if err != nil {
		return "", false
	}
	return string(plain), true
}

// bindWebhook is what the seal is bound to.
//
// A ciphertext copied into another source's row, or onto another webhook, fails
// to open here -- without any key having been broken. The same reasoning as a
// credential's binding, with the fields this entity has.
func bindWebhook(sourceID string, id shared.ID) []byte {
	return fmt.Appendf(nil, "%s|%s", sourceID, id)
}

// newSigningSecret is 32 bytes of randomness, base64url, prefixed so that one
// found in a log or a config file is recognizable for what it is.
func newSigningSecret() (string, error) {
	raw := make([]byte, signingSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", errs.InternalErr("could not generate a signing secret")
	}
	return signingSecretPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
