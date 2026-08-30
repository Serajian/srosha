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

func (r oneSource) UpdateSettings(context.Context, *source.Source) error { return nil }

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

// The three fields a customer owns change. The ceiling does not -- and this
// asserts on the entity rather than trusting the statement, because a use case
// that reads and re-sends the whole row is exactly how a ceiling gets carried
// somewhere it should not.
func TestChangingSettingsLeavesTheCeilingAlone(t *testing.T) {
	src := waiting()
	src.MaxPriority = shared.PriorityCritical
	src.AllowCustomAddress = true

	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Reconfigure("acme-alerts", "pages the on-call", nil, at); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	got := src

	if got.Name != "acme-alerts" || got.Description != "pages the on-call" {
		t.Errorf("name = %q, description = %q", got.Name, got.Description)
	}
	if got.MaxPriority != shared.PriorityCritical {
		t.Errorf("the ceiling moved to %v", got.MaxPriority)
	}
	if !got.AllowCustomAddress {
		t.Error("allow_custom_address was cleared by an edit")
	}
	if got.IsActive {
		t.Error("an edit switched the source on")
	}
	if !got.UpdatedAt.Equal(at) {
		t.Errorf("updated_at = %v", got.UpdatedAt)
	}
}

// A default address that no longer exists is the ordinary reason to edit a
// source, so the map has to be replaceable rather than append-only.
func TestADefaultAddressCanBeReplaced(t *testing.T) {
	src := waiting()
	src.DefaultAddresses = map[shared.Channel]string{shared.ChannelEmail: "old@acme.test"}

	err := src.Reconfigure(
		src.Name, "",
		map[shared.Channel]string{shared.ChannelEmail: "new@acme.test"},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	got := src
	if got.DefaultAddresses[shared.ChannelEmail] != "new@acme.test" {
		t.Errorf("address = %q", got.DefaultAddresses[shared.ChannelEmail])
	}
}

// An edit is all of it or none of it. A bad address must not leave the name
// already changed, because the customer would fix the address and never know
// the rename had gone through on its own.
func TestABadAddressLeavesTheWholeEditUndone(t *testing.T) {
	src := waiting()
	err := src.Reconfigure(
		"renamed", "",
		map[shared.Channel]string{shared.ChannelEmail: "not-an-address"},
		time.Now().UTC(),
	)
	if err == nil {
		t.Fatal("a bad address was accepted")
	}
	if src.Name != "acme-billing" {
		t.Errorf("the name changed anyway: %q", src.Name)
	}
}
