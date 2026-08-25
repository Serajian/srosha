package crypto_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/pkg/crypto"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, crypto.KeySize) }

func ring(t *testing.T, active string, ids ...string) *crypto.Keyring {
	t.Helper()

	keys := map[string][]byte{}
	for i, id := range ids {
		keys[id] = key(byte('a' + i))
	}
	k, err := crypto.NewKeyring(keys, active)
	if err != nil {
		t.Fatalf("NewKeyring() = %v", err)
	}
	return k
}

func TestASealedValueOpensAgain(t *testing.T) {
	k := ring(t, "1", "1")
	aad := []byte("src|email|cred")

	sealed, err := k.Seal([]byte("bot-token"), aad)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}

	got, err := k.Open(sealed, aad)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if string(got) != "bot-token" {
		t.Errorf("Open() = %q, want %q", got, "bot-token")
	}
}

func TestTheValueSaysWhichKeySealedIt(t *testing.T) {
	k := ring(t, "2", "1", "2")

	sealed, err := k.Seal([]byte("x"), nil)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}

	parts := strings.Split(sealed, ".")
	if len(parts) != 4 {
		t.Fatalf("got %d fields, want 4: %q", len(parts), sealed)
	}
	if parts[0] != "v1" {
		t.Errorf("version = %q, want v1", parts[0])
	}
	if parts[1] != "2" {
		t.Errorf("key id = %q, want the active key", parts[1])
	}
}

// The reason NeedsReseal has to exist: without it, every read would be followed
// by a write, because no two seals of the same value are ever equal.
func TestSealingTwiceNeverRepeatsItself(t *testing.T) {
	k := ring(t, "1", "1")

	first, err := k.Seal([]byte("same"), nil)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}
	second, err := k.Seal([]byte("same"), nil)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}
	if first == second {
		t.Error("two seals of one value are identical, so the nonce is not random")
	}
}

func TestAValueMovedSomewhereElseDoesNotOpen(t *testing.T) {
	k := ring(t, "1", "1")

	sealed, err := k.Seal([]byte("token"), []byte("source-a|email|cred-1"))
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}

	// The same ciphertext, read as if it belonged to another source.
	if _, err := k.Open(sealed, []byte("source-b|email|cred-1")); err == nil {
		t.Fatal("Open() succeeded with somebody else's identity")
	} else if !errors.Is(err, crypto.ErrCannotOpen) {
		t.Errorf("Open() = %v, want ErrCannotOpen", err)
	}
}

func TestATamperedValueDoesNotOpen(t *testing.T) {
	k := ring(t, "1", "1")

	sealed, err := k.Seal([]byte("token"), nil)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}

	parts := strings.Split(sealed, ".")
	parts[3] = flip(parts[3])
	if _, err := k.Open(strings.Join(parts, "."), nil); err == nil {
		t.Fatal("Open() succeeded on a changed ciphertext")
	}
}

func TestAValueNamingAKeyWeDoNotHold(t *testing.T) {
	sealer := ring(t, "1", "1")
	sealed, err := sealer.Seal([]byte("token"), nil)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}

	other := ring(t, "9", "9")
	if _, err := other.Open(sealed, nil); !errors.Is(err, crypto.ErrUnknownKey) {
		t.Errorf("Open() = %v, want ErrUnknownKey", err)
	}
}

func TestMalformedValues(t *testing.T) {
	k := ring(t, "1", "1")

	cases := map[string]string{
		"empty":         "",
		"too few parts": "v1.1.abc",
		"too many":      "v1.1.abc.def.ghi",
		"bad version":   "v2.1.YWJj.ZGVm",
		"bad base64":    "v1.1.!!!.ZGVm",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := k.Open(value, nil); err == nil {
				t.Errorf("Open(%q) succeeded", value)
			}
		})
	}
}

// Rotation, end to end: a value sealed with the old key still opens, says it
// wants resealing, and after that says it does not.
func TestRotationLeavesNothingUnreadable(t *testing.T) {
	old := ring(t, "1", "1")
	sealed, err := old.Seal([]byte("token"), nil)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}

	// The new key is added and the active id is moved onto it. Nothing else.
	both, err := crypto.NewKeyring(map[string][]byte{"1": key('a'), "2": key('b')}, "2")
	if err != nil {
		t.Fatalf("NewKeyring() = %v", err)
	}

	plain, err := both.Open(sealed, nil)
	if err != nil {
		t.Fatalf("Open() on the old key = %v", err)
	}
	if !both.NeedsReseal(sealed) {
		t.Error("NeedsReseal() = false for a value sealed with the old key")
	}

	resealed, err := both.Seal(plain, nil)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}
	if both.NeedsReseal(resealed) {
		t.Error("NeedsReseal() = true right after resealing")
	}

	got, err := both.Open(resealed, nil)
	if err != nil || string(got) != "token" {
		t.Errorf("Open() = %q, %v", got, err)
	}
}

func TestNeedsResealIgnoresWhatItCannotRead(t *testing.T) {
	k := ring(t, "1", "1")
	if k.NeedsReseal("not a sealed value") {
		t.Error("NeedsReseal() = true for something unparseable")
	}
}

func TestAKeyringIsCheckedWhenItIsBuilt(t *testing.T) {
	cases := map[string]struct {
		keys   map[string][]byte
		active string
		want   error
	}{
		"no keys":            {map[string][]byte{}, "1", crypto.ErrNoKeys},
		"active is missing":  {map[string][]byte{"1": key('a')}, "2", crypto.ErrNoActiveKey},
		"key is too short":   {map[string][]byte{"1": []byte("short")}, "1", crypto.ErrKeySize},
		"id has a separator": {map[string][]byte{"a.b": key('a')}, "a.b", crypto.ErrKeyID},
		"id is empty":        {map[string][]byte{"": key('a')}, "", crypto.ErrKeyID},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := crypto.NewKeyring(c.keys, c.active)
			if !errors.Is(err, c.want) {
				t.Errorf("NewKeyring() = %v, want %v", err, c.want)
			}
		})
	}
}

// A key must never reach a log or an error message.
func TestAFailureNeverEchoesTheKey(t *testing.T) {
	secret := "SUPERSECRETKEYMATERIAL"
	_, err := crypto.NewKeyring(map[string][]byte{"1": []byte(secret)}, "1")
	if err == nil {
		t.Fatal("NewKeyring() accepted a key of the wrong length")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error carries the key: %v", err)
	}
}

func flip(s string) string {
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}
