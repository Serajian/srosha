package usecase_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

// fakeIssuer records what was written, so a test can check that the key itself
// never was.
type fakeIssuer struct {
	mu     sync.Mutex
	keys   []source.Key
	hashes []string
}

func (f *fakeIssuer) Create(_ context.Context, k *source.Key, keyHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, *k)
	f.hashes = append(f.hashes, keyHash)
	return nil
}

func (f *fakeIssuer) ListBySourceID(_ context.Context, id string) ([]source.Key, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []source.Key{}
	for _, k := range f.keys {
		if k.SourceID == id {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeIssuer) Revoke(_ context.Context, id shared.ID, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.keys {
		if f.keys[i].ID == id {
			f.keys[i].RevokedAt = &now
			return nil
		}
	}
	return errs.NotFoundErr("no such key").WithErr(source.ErrKeyNotFound)
}

// mintN hands out a different key every time, so a test can tell two apart.
type mintN struct {
	mu sync.Mutex
	n  int
}

func (m *mintN) Mint() (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	return fmt.Sprintf("srosha_sk_%03d", m.n), fmt.Sprintf("hash_%03d", m.n), nil
}

type keyRig struct {
	keys     *usecase.Keys
	issuer   *fakeIssuer
	log      *auditLog
	actor    *user.User
	stranger *user.User
	sourceID string
}

func newKeyRig(t *testing.T) *keyRig {
	t.Helper()

	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	actor, err := user.New(
		shared.ID("01K0ACCT0000000000000000AB"), "me@acme.test", user.RoleCustomer, at,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	stranger, err := user.New(
		shared.ID("01K0ACCT0000000000000000AC"), "them@acme.test", user.RoleCustomer, at,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}

	log := &auditLog{}
	gate := usecase.NewGate(log, nil, seqIDs(), fixedNow(at))
	repo := fakeSources{byID: map[string]*source.Source{}}
	sources := usecase.NewSources(repo, gate, seqIDs(), fixedNow(at))

	src, err := sources.Register(
		context.Background(), actor, usecase.SourceRegistration{Name: "acme"},
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	log.entries = nil // the source's own row is not what these tests count

	issuer := &fakeIssuer{}
	return &keyRig{
		keys:     usecase.NewKeys(issuer, sources, &mintN{}, gate, seqIDs(), fixedNow(at)),
		issuer:   issuer,
		log:      log,
		actor:    actor,
		stranger: stranger,
		sourceID: src.ID,
	}
}

// The key is returned once, from the call that made it. srosha keeps a hash,
// so there is no second chance and the page has to say so.
func TestAKeyIsHandedBackOnceAndOnlyStoredHashed(t *testing.T) {
	rig := newKeyRig(t)

	key, k, err := rig.keys.Issue(context.Background(), rig.actor, rig.sourceID, "laptop")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if key == "" {
		t.Fatal("no key came back")
	}
	if k.Label != "laptop" {
		t.Errorf("label = %q", k.Label)
	}

	for _, stored := range rig.issuer.hashes {
		if strings.Contains(stored, key) {
			t.Error("the key itself was stored, not a hash of it")
		}
	}
}

// Two at once is the whole reason keys are rows: rotation is issue the second,
// move, revoke the first, with no window where messages are refused.
func TestASourceMayHoldTwoKeys(t *testing.T) {
	rig := newKeyRig(t)
	ctx := context.Background()

	for _, label := range []string{"old", "new"} {
		if _, _, err := rig.keys.Issue(ctx, rig.actor, rig.sourceID, label); err != nil {
			t.Fatalf("Issue(%s): %v", label, err)
		}
	}

	got, err := rig.keys.List(ctx, rig.actor, rig.sourceID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2", len(got))
	}
}

// Ownership is checked on the source, not on the key: a key id says nothing
// about who may touch it.
func TestNobodyIssuesAKeyForSomebodyElsesSource(t *testing.T) {
	rig := newKeyRig(t)

	if _, _, err := rig.keys.Issue(
		context.Background(), rig.stranger, rig.sourceID, "theirs",
	); err == nil {
		t.Fatal("a key was issued for a source the actor does not own")
	}
	if len(rig.issuer.hashes) != 0 {
		t.Error("a key was written despite the refusal")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused issue still wrote an audit row")
	}
}

func TestRevokingAKeyLeavesAnAuditRow(t *testing.T) {
	rig := newKeyRig(t)
	ctx := context.Background()

	_, k, err := rig.keys.Issue(ctx, rig.actor, rig.sourceID, "laptop")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	before := len(rig.log.entries)

	if err := rig.keys.Revoke(ctx, rig.actor, rig.sourceID, k.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if len(rig.log.entries) != before+1 {
		t.Fatalf("revocation wrote %d rows", len(rig.log.entries)-before)
	}
	if rig.log.entries[before].Verb != usecase.ActKeyRevoke {
		t.Errorf("verb = %q", rig.log.entries[before].Verb)
	}
}

// A key id belongs to a source. Naming one that does not is refused rather than
// revoked, which is what stops somebody revoking another customer's key by
// guessing its id on a source they do own.
func TestRevokingAKeyThatIsNotThisSourcesIsRefused(t *testing.T) {
	rig := newKeyRig(t)

	err := rig.keys.Revoke(
		context.Background(), rig.actor, rig.sourceID, shared.ID("01K0KEY00000000000000000AB"),
	)
	if err == nil {
		t.Fatal("a key that belongs to no source of ours was revoked")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused revocation still wrote an audit row")
	}
}
