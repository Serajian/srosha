package system

import (
	"crypto/rand"
	"io"
	"sync"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/oklog/ulid/v2"
)

// IDs mints the identifiers everything in this service is found by.
//
// ULID, and the choice is load-bearing rather than a taste in identifiers. The
// first ten characters are the millisecond it was made, so ids sort by time --
// which is what lets a listing page on the id alone:
//
//	ORDER BY id           the creation order, with no second column
//	WHERE id > @after     the next page, with no offset to count past
//
// A uuid v4 would make both of those impossible and force a created_at index
// beside every primary key.
type IDs struct {
	// entropy is monotonic AND locked. Monotonic so two ids made in the same
	// millisecond still order by which came first -- without it their random
	// halves decide, and "sorted by id" stops meaning "in the order they were
	// made". Locked because the gateway answers every request on its own
	// goroutine and this reader keeps state between calls.
	mu      sync.Mutex
	entropy io.Reader

	now shared.NowFunc
}

// NewIDs takes the clock rather than reading it, so an id's timestamp and the
// row's created_at come from the same place and cannot disagree.
func NewIDs(now shared.NowFunc) (*IDs, error) {
	if now == nil {
		return nil, errs.InternalErr("id generator has no clock")
	}

	// crypto/rand, not math/rand. An id is quoted back to a customer and used
	// as a page cursor, so a predictable one lets somebody walk the sequence
	// and learn how much traffic another source is sending.
	return &IDs{
		entropy: ulid.Monotonic(rand.Reader, 0),
		now:     now,
	}, nil
}

// Generate is shared.IDFunc: bootstrap hands this method to whoever needs an id.
//
// It cannot fail, because the port cannot. ulid.New has one error -- the
// monotonic entropy running out of room inside a single millisecond, which
// needs the random half to already be all ones -- and the answer to it is a
// fresh non-monotonic id rather than a panic on a live request. That id is
// still unique and still sorts to the right millisecond; only its order
// against its neighbors in that millisecond is arbitrary.
func (g *IDs) Generate() shared.ID {
	at := ulid.Timestamp(g.now())

	g.mu.Lock()
	defer g.mu.Unlock()

	id, err := ulid.New(at, g.entropy)
	if err != nil {
		// crypto/rand does not return errors -- since Go 1.24 it crashes the
		// process rather than handing back a value it cannot vouch for -- so
		// this cannot fail for a reason that leaves us running.
		id = ulid.MustNew(at, rand.Reader)
	}
	return shared.ID(id.String())
}
