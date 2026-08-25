// Package crypto seals a value so that it can be read back, and so that the
// value itself says which key sealed it.
//
// It is deliberately not a general encryption library. It does one thing: hold
// a set of keys, seal with the current one, and open with whichever one the
// value names.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Serajian/srosha/pkg/errs"
)

// Keyring holds every key that can open a value, and the one new values are
// sealed with.
//
// A set rather than a single key, because changing the key is a *when* and not
// an *if*. With one key, changing it means stopping, opening everything with the
// old one and sealing it again with the new -- an outage that can fail halfway
// and leave two kinds of value that cannot be told apart. With a set, the new
// key is added, the active id is pointed at it, and old values are resealed as
// they are touched.
type Keyring struct {
	keys   map[string]cipher.AEAD
	active string
}

// NewKeyring checks every key up front. A key that is the wrong length or an
// active id naming a key that is not there are both configuration mistakes, and
// the only good time to find them is before the process claims to be running.
func NewKeyring(keys map[string][]byte, activeID string) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, errs.InternalErr("no encryption keys configured").WithErr(ErrNoKeys)
	}

	ring := make(map[string]cipher.AEAD, len(keys))
	for id, key := range keys {
		if id == "" || strings.Contains(id, separator) {
			return nil, errs.InternalErr("encryption key id is not usable").
				WithErr(ErrKeyID).
				WithStr(fmt.Sprintf("id %q", id))
		}
		if len(key) != KeySize {
			// The id, never the key: this error is logged.
			return nil, errs.InternalErr("encryption key is the wrong length").
				WithErr(ErrKeySize).
				WithStr(fmt.Sprintf("key %q is %d bytes, want %d", id, len(key), KeySize))
		}

		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, errs.InternalErr("encryption key could not be used").
				WithErr(err).
				WithStr(fmt.Sprintf("key %q", id))
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, errs.InternalErr("encryption key could not be used").
				WithErr(err).
				WithStr(fmt.Sprintf("key %q", id))
		}
		ring[id] = aead
	}

	if _, ok := ring[activeID]; !ok {
		return nil, errs.InternalErr("the active encryption key is not in the keyring").
			WithErr(ErrNoActiveKey).
			WithStr(fmt.Sprintf("active %q", activeID))
	}

	return &Keyring{keys: ring, active: activeID}, nil
}

// ActiveID is which key new values are sealed with.
func (k *Keyring) ActiveID() string { return k.active }

// Seal returns the value to store: version, key id, nonce and ciphertext, in
// that order.
//
// aad is bound into the seal without being encrypted: it is not stored here and
// has to be supplied again to open. Whoever seals decides what it is; the point
// is that a ciphertext moved somewhere it does not belong no longer opens.
func (k *Keyring) Seal(plaintext, aad []byte) (string, error) {
	aead := k.keys[k.active]

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errs.InternalErr("the value could not be encrypted").WithErr(err)
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, aad)

	return strings.Join([]string{
		version,
		k.active,
		encoding.EncodeToString(nonce),
		encoding.EncodeToString(ciphertext),
	}, separator), nil
}

// Open reads a value back, with whichever key the value names.
//
// GCM authenticates, so a value that has been tampered with -- or moved to
// another row, which changes the aad -- fails to open rather than opening to
// rubbish. Every one of those failures is the same error on purpose: which of
// them it was is not something the caller can act on differently.
func (k *Keyring) Open(value string, aad []byte) ([]byte, error) {
	ver, id, nonce, ciphertext, err := split(value)
	if err != nil {
		return nil, err
	}
	if ver != version {
		return nil, errs.InternalErr("the value could not be decrypted").
			WithErr(ErrOldVersion).
			WithStr(fmt.Sprintf("version %q", ver))
	}

	aead, ok := k.keys[id]
	if !ok {
		// Almost always a key dropped from the environment while values still
		// name it, so the id is worth having in the log.
		return nil, errs.InternalErr("the value could not be decrypted").
			WithErr(ErrUnknownKey).
			WithStr(fmt.Sprintf("key %q", id))
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errs.InternalErr("the value could not be decrypted").
			WithErr(ErrMalformed).
			WithStr("nonce is the wrong length")
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, errs.InternalErr("the value could not be decrypted").WithErr(ErrCannotOpen)
	}
	return plaintext, nil
}

// NeedsReseal says whether a value was sealed with a key that is no longer the
// active one.
//
// This is what makes rotation cost nothing on the ordinary path: the key id is
// inside the value, so asking costs no query and no extra column, and the caller
// writes only when the answer is yes. Without it every read would be followed by
// a write, because the nonce is random and sealing the same value twice never
// produces the same string.
//
// A value that will not parse is not resealable, so it answers false. Open has
// already refused it by then.
func (k *Keyring) NeedsReseal(value string) bool {
	_, id, _, _, err := split(value)
	if err != nil {
		return false
	}
	return id != k.active
}

// encoding has no padding and no separator character in its alphabet, so an
// encoded field can never be mistaken for two.
var encoding = base64.RawURLEncoding

func split(value string) (ver, id string, nonce, ciphertext []byte, err error) {
	parts := strings.Split(value, separator)
	if len(parts) != fields {
		return "", "", nil, nil, errs.InternalErr("the value could not be decrypted").
			WithErr(ErrMalformed).
			WithStr(fmt.Sprintf("got %d fields, want %d", len(parts), fields))
	}

	nonce, err = encoding.DecodeString(parts[2])
	if err != nil {
		return "", "", nil, nil, errs.InternalErr("the value could not be decrypted").
			WithErr(ErrMalformed).
			WithStr("nonce is not valid base64")
	}
	ciphertext, err = encoding.DecodeString(parts[3])
	if err != nil {
		return "", "", nil, nil, errs.InternalErr("the value could not be decrypted").
			WithErr(ErrMalformed).
			WithStr("ciphertext is not valid base64")
	}
	return parts[0], parts[1], nonce, ciphertext, nil
}
