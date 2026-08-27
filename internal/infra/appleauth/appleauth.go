// Package appleauth makes the provider token APNs authenticates with: a JWT
// this service signs itself, with the p8 key from Apple's developer portal.
//
// It is the mirror of internal/infra/googleauth and deliberately shaped the
// same, but nothing here talks to anybody. Apple has no token endpoint -- a
// provider token is signed locally and simply presented. What makes this a
// resource rather than a function is the clock around it: Apple wants a token
// refreshed at least hourly and refuses one made more often than every twenty
// minutes, so the last token has to be kept and reused between messages.
package appleauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Identity is which key is signing and who it belongs to. Both come from the
// developer portal and neither is secret on its own -- the p8 file is.
type Identity struct {
	// KeyID names the p8 file. It goes in the JWT header so Apple knows which
	// of an account's keys to check the signature with.
	KeyID string

	// TeamID is the developer account. It is the token's only claim beyond the
	// time it was made.
	TeamID string
}

func (i Identity) validate() error {
	switch {
	case strings.TrimSpace(i.KeyID) == "":
		return errors.New("appleauth: no key id")
	case strings.TrimSpace(i.TeamID) == "":
		return errors.New("appleauth: no team id")
	}
	return nil
}

// Minter hands out provider tokens, one Source per identity.
//
// Safe for concurrent use: the dispatcher resolves a sender per message, and
// several may want the same identity at once.
type Minter struct {
	now func() time.Time

	mu      sync.Mutex
	sources map[[32]byte]*Source
}

// NewMinter takes the clock rather than reading it, so a test can watch a token
// age without waiting three quarters of an hour.
func NewMinter(now func() time.Time) (*Minter, error) {
	if now == nil {
		return nil, errors.New("appleauth: no clock")
	}
	return &Minter{now: now, sources: make(map[[32]byte]*Source)}, nil
}

// Open prepares a token source for one identity, and signs nothing: the first
// token is made when one is first asked for.
//
// Calling it again with the same key and identity hands back the same source,
// which is the point -- a second source would mean a second token where one was
// still good, and Apple counts those.
func (m *Minter) Open(p8 []byte, id Identity) (*Source, error) {
	if err := id.validate(); err != nil {
		return nil, err
	}

	key, err := parseKey(p8)
	if err != nil {
		return nil, err
	}

	// The key material never becomes a map key: this is a digest of it, so what
	// the map copies and compares is not a private key.
	sum := sha256.Sum256(append([]byte(id.KeyID+"\x00"+id.TeamID+"\x00"), p8...))

	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sources[sum]; ok {
		return s, nil
	}
	if len(m.sources) >= maxCachedIdentities {
		clear(m.sources)
	}

	s := &Source{key: key, id: id, now: m.now}
	m.sources[sum] = s
	return s, nil
}

// Source is one identity's supply of provider tokens.
type Source struct {
	key *ecdsa.PrivateKey
	id  Identity
	now func() time.Time

	mu     sync.Mutex
	token  string
	minted time.Time
}

// Identity is who this signs as.
func (s *Source) Identity() Identity { return s.id }

// Token returns one that is still inside Apple's refresh window, signing a new
// one only when the last has aged out or there has not been one yet.
func (s *Source) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.token != "" && now.Sub(s.minted) < tokenLifetime {
		return s.token, nil
	}

	token, err := s.sign(now)
	if err != nil {
		return "", err
	}
	s.token, s.minted = token, now
	return token, nil
}

// Expire throws the held token away so the next Token signs a fresh one.
//
// It exists for one answer: APNs saying ExpiredProviderToken. That is worth
// another attempt, but only with a different token -- and without this the
// cache would hand the same stale one back until the attempts ran out.
func (s *Source) Expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.minted = "", time.Time{}
}

// sign makes the JWT itself.
//
// Written here rather than taken from a library because only one direction is
// needed. This produces a token with one algorithm and two claims and never
// reads one back, and a jwt package is mostly the parsing and validation that
// a verifier needs. pkg/crypto is hand-written for the same reason.
func (s *Source) sign(issued time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{
		"alg": "ES256", "kid": s.id.KeyID, "typ": "JWT",
	})
	if err != nil {
		return "", fmt.Errorf("appleauth: %w", err)
	}
	claims, err := json.Marshal(map[string]any{
		"iss": s.id.TeamID, "iat": issued.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("appleauth: %w", err)
	}

	signing := encode(header) + "." + encode(claims)

	// ES256 is ECDSA on P-256 over a SHA-256 digest, and its signature is r and
	// s as fixed 32-byte halves -- not the ASN.1 pair Go returns by default.
	sum := sha256.Sum256([]byte(signing))
	r, sig, err := ecdsa.Sign(rand.Reader, s.key, sum[:])
	if err != nil {
		return "", errors.New("appleauth: could not sign a provider token")
	}

	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	sig.FillBytes(raw[32:])

	return signing + "." + encode(raw), nil
}

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// parseKey reads the .p8 file Apple hands out: a PKCS#8 PEM around an ECDSA
// key on P-256.
//
// Checked here rather than at the first signature, so a wrong file is a
// configuration answer when the credential is opened instead of a failure that
// looks temporary when a message is going out.
func parseKey(p8 []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(p8)
	if block == nil {
		return nil, errors.New("appleauth: the signing key is not pem")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// The error is about the bytes of a private key and is not carried.
		return nil, errors.New("appleauth: the signing key is not usable")
	}

	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		// An RSA key here is the ordinary mistake: it is what Google's service
		// account carries, and the two files look alike from the outside.
		return nil, errors.New("appleauth: the signing key is not an ecdsa key")
	}
	if key.Curve != elliptic.P256() {
		// ES256 names the curve as well as the digest, so another one would
		// produce a signature Apple has no way to check.
		return nil, errors.New("appleauth: the signing key is not on p-256")
	}
	return key, nil
}
