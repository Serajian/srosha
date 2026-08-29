package source_test

import (
	"context"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// waiting is a source as it is the moment somebody registers one: switched off,
// never approved.
func waiting() *source.Source {
	return &source.Source{
		ID:               "01K0SRC0000000000000000000",
		OwnerUserID:      shared.ID("01K0ACCT0000000000000000AB"),
		Name:             "acme-billing",
		MaxPriority:      shared.PriorityNormal,
		IsActive:         false,
		DefaultAddresses: map[shared.Channel]string{},
		CreatedAt:        time.Now().UTC(),
	}
}

type oneSource struct{ src *source.Source }

func (r oneSource) Create(context.Context, *source.Source) error { return nil }

func (r oneSource) ReadByID(_ context.Context, id string) (*source.Source, error) {
	if r.src == nil || r.src.ID != id {
		return nil, errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	return r.src, nil
}

func (r oneSource) ListByOwner(context.Context, shared.ID) ([]source.Source, error) {
	return nil, nil
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string) (bool, error) { return true, nil }

// A customer configures a source before it is approved, not after. Otherwise
// registering one ends in waiting, and the operator approves a shell with
// nothing set up in it.
func TestAnUnapprovedSourceCanStillBeConfigured(t *testing.T) {
	src := waiting()
	svc := source.NewService(oneSource{src: src}, allowAll{})

	got, err := svc.Manage(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("Manage: %v", err)
	}
	if got.ID != src.ID {
		t.Errorf("id = %q", got.ID)
	}
}

// And it still may not send.
func TestAnUnapprovedSourceMayNotSend(t *testing.T) {
	src := waiting()
	svc := source.NewService(oneSource{src: src}, allowAll{})

	if _, err := svc.Admit(context.Background(), src.ID); err == nil {
		t.Fatal("Admit: an unapproved source was allowed to send")
	}
}

// Manage is not a way around the active check, only a different question. A
// source that is not there is still not there.
func TestManagingASourceThatIsNotThere(t *testing.T) {
	svc := source.NewService(oneSource{}, allowAll{})

	if _, err := svc.Manage(context.Background(), "01K0SRC0000000000000000000"); err == nil {
		t.Fatal("Manage: want an error for a source that does not exist")
	}
}
