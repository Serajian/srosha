package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/auth"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/pkg/errs"
)

// Scheme holds nothing, so one is enough for the whole file.
var scheme = auth.NewScheme()

func TestMintedKeysAreAcceptedByOurOwnParser(t *testing.T) {
	key, hash, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}

	if !strings.HasPrefix(key, "srosha_") {
		t.Errorf("key = %q, want the prefix a scanner can be taught", key)
	}
	if len(key) != len("srosha_")+43 {
		t.Errorf("key is %d chars, want %d", len(key), len("srosha_")+43)
	}

	parsed, err := scheme.Parse(key)
	if err != nil {
		t.Fatalf("our own key was refused: %v", err)
	}
	if parsed != hash {
		t.Error("Parse and Mint disagree about the hash, so nothing would ever authenticate")
	}
}

// Two keys minted in a row must not share anything. A generator that repeats
// itself hands one customer another customer's identity.
func TestEveryKeyIsDifferent(t *testing.T) {
	seen := make(map[string]bool, 128)

	for range 128 {
		key, hash, err := scheme.Mint()
		if err != nil {
			t.Fatalf("Mint() error = %v", err)
		}
		if seen[key] || seen[hash] {
			t.Fatalf("a key or its hash repeated: %q", key)
		}
		seen[key], seen[hash] = true, true
	}
}

func TestHashIsStableAndSaysNothingAboutTheKey(t *testing.T) {
	key, hash, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}

	if scheme.Hash(key) != hash {
		t.Error("hashing the same key twice gave two answers")
	}
	if len(hash) != 64 {
		t.Errorf("hash is %d chars, want 64 -- sha-256 in hex", len(hash))
	}
	// The hash is what we store and what may end up in a query log. It must not
	// be possible to read the key back out of it.
	if strings.Contains(hash, strings.TrimPrefix(key, "srosha_")) {
		t.Error("the hash contains the key")
	}
}

// A string of the wrong shape cannot be one of ours, so it is refused before
// anything touches the database -- and refused with exactly the answer an
// unknown key gets, or the API becomes a probe for our format.
func TestBadlyShapedKeysAreRefusedLikeUnknownOnes(t *testing.T) {
	long := "srosha_" + strings.Repeat("a", 44)

	tests := []struct {
		name      string
		presented string
	}{
		{"empty", ""},
		{"no prefix", strings.Repeat("a", 43)},
		{"wrong prefix", "srosha-" + strings.Repeat("a", 43)},
		{"too short", "srosha_" + strings.Repeat("a", 42)},
		{"too long", long},
		{"not base64url", "srosha_" + strings.Repeat("!", 43)},
		{"padded base64", "srosha_" + strings.Repeat("a", 41) + "=="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := scheme.Parse(tt.presented)
			if err == nil {
				t.Fatalf("Parse() = %q, want it refused", got)
			}
			if !errors.Is(err, source.ErrUnknownKey) {
				t.Errorf("errors.Is(ErrUnknownKey) = false, got %v", err)
			}
			if !errs.IsType(err, errs.ErrUnauthorized) {
				t.Errorf("type = %v, want unauthorized", errs.TypeOf(err))
			}
		})
	}
}
