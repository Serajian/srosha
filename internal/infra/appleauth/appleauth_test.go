package appleauth_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/appleauth"
)

var identity = appleauth.Identity{KeyID: "ABC1234567", TeamID: "TEAM123456"}

// p8 writes the file Apple hands out: PKCS#8 PEM around an ECDSA key on P-256.
func p8(t *testing.T) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), key
}

// clock hands out a time a test moves by hand, so a token can be watched aging
// without waiting three quarters of an hour.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func minter(t *testing.T, c *clock) *appleauth.Minter {
	t.Helper()

	m, err := appleauth.NewMinter(c.now)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}
	return m
}

func TestAProviderTokenIsSignedOnceAndThenReused(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	key, _ := p8(t)

	source, err := minter(t, c).Open(key, identity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	first, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	c.at = c.at.Add(44 * time.Minute)
	again, err := source.Token()
	if err != nil {
		t.Fatalf("Token again: %v", err)
	}
	if again != first {
		t.Error("a token still inside the window was signed again")
	}
}

// Apple refuses a token signed more often than every twenty minutes and wants
// one no older than an hour, so the window has to sit between them.
func TestATokenPastTheWindowIsSignedAgain(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	key, _ := p8(t)

	source, _ := minter(t, c).Open(key, identity)
	first, _ := source.Token()

	c.at = c.at.Add(46 * time.Minute)
	again, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if again == first {
		t.Error("a token past the window was handed back")
	}
}

// The whole reason this package holds state: a sender is built per message, so
// the same identity arriving again must not mean a second token.
func TestTheSameIdentityOpensToTheSameSource(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	key, _ := p8(t)
	m := minter(t, c)

	first, err := m.Open(key, identity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	firstToken, _ := first.Token()

	second, err := m.Open(key, identity)
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	secondToken, _ := second.Token()

	if firstToken != secondToken {
		t.Error("the same identity signed two tokens")
	}
}

func TestTwoIdentitiesDoNotShareAToken(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	key, _ := p8(t)
	m := minter(t, c)

	one, _ := m.Open(key, identity)
	two, err := m.Open(key, appleauth.Identity{KeyID: "ZZZ7654321", TeamID: identity.TeamID})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	oneToken, _ := one.Token()
	twoToken, _ := two.Token()
	if oneToken == twoToken {
		t.Error("two key ids shared one token")
	}
}

// Expire is what makes ExpiredProviderToken worth another attempt: without it
// the cache would present the same stale token until the attempts ran out.
func TestExpireForcesTheNextTokenToBeNew(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	key, _ := p8(t)

	source, _ := minter(t, c).Open(key, identity)
	first, _ := source.Token()

	source.Expire()

	// The clock has not moved: only Expire can be the reason this differs.
	c.at = c.at.Add(time.Second)
	again, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if again == first {
		t.Error("the discarded token came back")
	}
}

// The signature is checked against the key that made it, because this is the
// one thing a library would have done for us.
func TestTheTokenIsAValidES256JWT(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	key, private := p8(t)

	source, _ := minter(t, c).Open(key, identity)
	token, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}

	var header struct{ Alg, Kid, Typ string }
	decodeInto(t, parts[0], &header)
	if header.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", header.Alg)
	}
	if header.Kid != identity.KeyID {
		t.Errorf("kid = %q, want the key id", header.Kid)
	}
	if header.Typ != "JWT" {
		t.Errorf("typ = %q, want JWT", header.Typ)
	}

	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
	}
	decodeInto(t, parts[1], &claims)
	if claims.Iss != identity.TeamID {
		t.Errorf("iss = %q, want the team id", claims.Iss)
	}
	if claims.Iat != c.at.Unix() {
		t.Errorf("iat = %d, want %d", claims.Iat, c.at.Unix())
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("signature is not base64url: %v", err)
	}
	if len(sig) != 64 {
		// r and s as fixed 32-byte halves, not the ASN.1 pair Go returns.
		t.Fatalf("signature is %d bytes, want 64", len(sig))
	}

	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(&private.PublicKey, sum[:], r, s) {
		t.Error("the signature does not check out against the key that made it")
	}
}

func decodeInto(t *testing.T, part string, target any) {
	t.Helper()

	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("part is not base64url: %v", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("part is not json: %v", err)
	}
}

// Checked when the credential is opened, not at the first signature: a wrong
// file is a configuration answer, not a message that failed.
func TestWhatIsNotASigningKey(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	m := minter(t, c)

	t.Run("not pem at all", func(t *testing.T) {
		if _, err := m.Open([]byte("AuthKey_ABC123.p8"), identity); err == nil {
			t.Fatal("Open: want an error")
		}
	})

	t.Run("pem around nothing usable", func(t *testing.T) {
		raw := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("nope")})
		if _, err := m.Open(raw, identity); err == nil {
			t.Fatal("Open: want an error")
		}
	})

	// The ordinary mistake: it is what a Google service account carries, and
	// the two files look alike from the outside.
	t.Run("an rsa key", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
		}
		raw := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

		_, err = m.Open(raw, identity)
		if err == nil {
			t.Fatal("Open: want an error")
		}
		if !strings.Contains(err.Error(), "ecdsa") {
			t.Errorf("error = %q, want it to say what kind of key was wanted", err)
		}
	})

	// ES256 names the curve as well as the digest.
	t.Run("an ecdsa key on another curve", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
		}
		raw := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

		if _, err := m.Open(raw, identity); err == nil {
			t.Fatal("Open: want an error")
		}
	})
}

func TestAnIdentityNeedsBothNames(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	m := minter(t, c)
	key, _ := p8(t)

	if _, err := m.Open(key, appleauth.Identity{TeamID: "TEAM123456"}); err == nil {
		t.Error("Open with no key id succeeded")
	}
	if _, err := m.Open(key, appleauth.Identity{KeyID: "ABC1234567"}); err == nil {
		t.Error("Open with no team id succeeded")
	}
}

func TestAMinterNeedsAClock(t *testing.T) {
	if _, err := appleauth.NewMinter(nil); err == nil {
		t.Error("NewMinter with no clock succeeded")
	}
}

// A p8 file is a private key, so nothing from it is quoted back.
func TestTheKeyNeverReachesAnError(t *testing.T) {
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	key, _ := p8(t)

	// Valid PEM, corrupt contents: the file's own bytes are in play.
	broken := append([]byte{}, key...)
	broken = []byte(strings.Replace(string(broken), "PRIVATE KEY-----\n",
		"PRIVATE KEY-----\nAAAA", 1))

	_, err := minter(t, c).Open(broken, identity)
	if err == nil {
		t.Fatal("Open: want an error")
	}
	if strings.Contains(err.Error(), "AAAA") || strings.Contains(err.Error(), "BEGIN") {
		t.Errorf("error = %q, key material leaked into it", err)
	}
}
