package shared

import (
	"fmt"
	"strings"

	"github.com/Serajian/srosha/pkg/errs"
)

// ID is the canonical identifier used across the system.
//
// Its shape is a ULID -- 26 characters of Crockford base32 -- but this package
// deliberately does not import a ULID library. Generation lives behind
// port.IDGenerator and is implemented in adapter/system. That keeps core free
// of third-party code, and lets a test inject a generator with a fixed
// sequence so assertions stay deterministic.
type ID string

const (
	idLength      = 26
	crockfordBase = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // no I, L, O, U
)

// ParseID validates and normalises an untrusted string into an ID.
//
// Call it wherever a string arrives from outside: a gRPC request, a queue
// message, a URL path. Do NOT call it on the read path from our own database.
// A row that was valid when written should stay loadable even if this rule
// later tightens, and a corrupt row is an internal fault rather than bad
// client input -- so it would be misclassified as ErrInvalidInput here.
func ParseID(s string) (ID, error) {
	if len(s) != idLength {
		return "", errs.InvalidInputErr("invalid id").
			WithErr(ErrInvalidID).
			WithStr(fmt.Sprintf("expected %d chars, got %d", idLength, len(s)))
	}

	upper := strings.ToUpper(s)
	for _, r := range upper {
		if !strings.ContainsRune(crockfordBase, r) {
			return "", errs.InvalidInputErr("invalid id").
				WithErr(ErrInvalidID).
				WithStr(fmt.Sprintf("illegal character %q", r))
		}
	}
	return ID(upper), nil
}

func (id ID) String() string { return string(id) }

func (id ID) IsZero() bool { return id == "" }
