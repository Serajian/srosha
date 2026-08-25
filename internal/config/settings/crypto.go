package settings

import (
	"encoding/base64"
	"fmt"
	"slices"

	"github.com/Serajian/srosha/pkg/crypto"
	"github.com/Serajian/srosha/pkg/env"
)

// Crypto is the set of keys that open stored secrets, and which of them new
// values are sealed with.
//
// A set rather than one key, because a stored value names the key that produced
// it. Changing the key is then: add the new one, point ActiveID at it, and let
// old values reseal themselves as they are read.
type Crypto struct {
	Keys     map[string][]byte
	ActiveID string
}

// LoadCrypto reads the keyring. Both keys are required: a deployment missing
// them cannot read a single stored credential, and the difference between
// finding that at boot and finding it on the first send is the difference
// between seconds and an outage nobody was watching for.
//
// Key material is standard base64 here, not the encoding used inside a sealed
// value: this is what a person types, and it is what `openssl rand -base64 32`
// hands back.
func LoadCrypto(r *env.Reader) Crypto {
	raw := map[string]env.Secret{}
	r.JSON("CRYPTO_KEYS", &raw)

	c := Crypto{
		Keys:     make(map[string][]byte, len(raw)),
		ActiveID: r.Required("CRYPTO_KEY_ID"),
	}

	// Sorted, so a deployment with two bad keys reports them in the same order
	// every time rather than in whatever order the map iterates.
	for _, id := range sortedIDs(raw) {
		key, err := base64.StdEncoding.DecodeString(raw[id].Reveal())
		if err != nil {
			// The id, never the value.
			r.Check(false, "NOTIF_CRYPTO_KEYS: key %q is not valid base64", id)
			continue
		}
		if len(key) != crypto.KeySize {
			r.Check(false, "NOTIF_CRYPTO_KEYS: key %q is %d bytes, must be %d",
				id, len(key), crypto.KeySize)
			continue
		}
		c.Keys[id] = key
	}

	r.Check(len(raw) > 0, "NOTIF_CRYPTO_KEYS is required")

	// Checked here as well as in NewKeyring, because this is the layer that can
	// name the environment variable that is wrong.
	if c.ActiveID != "" && len(c.Keys) > 0 {
		if _, ok := c.Keys[c.ActiveID]; !ok {
			r.Check(false, "NOTIF_CRYPTO_KEY_ID is %q, which is not a key in NOTIF_CRYPTO_KEYS",
				c.ActiveID)
		}
	}

	return c
}

// String makes sure a whole config struct can be logged without spilling the
// keyring: the ids are useful, the key material never is.
func (c Crypto) String() string {
	return fmt.Sprintf("Crypto{Keys:%v, ActiveID:%q}", sortedIDs(c.Keys), c.ActiveID)
}

func sortedIDs[V any](m map[string]V) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
