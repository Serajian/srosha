package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/usecase"
)

// The operator's view carries no message content. Asserted on what comes back
// rather than on the query, because the query is what a later edit changes.
func TestAnOperatorSeesNoMessageContent(t *testing.T) {
	rig := newOperatorRig(t)

	got, err := rig.ops.Deliveries(context.Background(), rig.admin, rig.sourceID, rig.messageID)
	if err != nil {
		t.Fatalf("Deliveries: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no deliveries came back")
	}

	if strings.Contains(got[0].MaskedAddress, "billing@acme.test") {
		t.Errorf("the full address came back: %q", got[0].MaskedAddress)
	}
	if !strings.Contains(got[0].MaskedAddress, "…") {
		t.Errorf("the address is not masked: %q", got[0].MaskedAddress)
	}
}

// The type is the other half of the guarantee: even a use case that forgot to
// mask could not put the seeded body anywhere in what it returns, because
// neither OperatorMessage nor OperatorDelivery has a field to hold it.
func TestAnOperatorSeesNoMessageBody(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	messages, err := rig.ops.Messages(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("no messages came back")
	}

	deliveries, err := rig.ops.Deliveries(ctx, rig.admin, rig.sourceID, rig.messageID)
	if err != nil {
		t.Fatalf("Deliveries: %v", err)
	}

	for _, m := range messages {
		if m.ID == "" {
			t.Error("a message came back with no id")
		}
	}
	for _, d := range deliveries {
		if strings.Contains(d.MaskedAddress, "billing@acme.test") {
			t.Errorf("the full address came back: %q", d.MaskedAddress)
		}
	}
}

// Messages aggregates over the seeded delivery: one message, one channel, no
// failures yet.
func TestMessagesSummarizesWithoutContent(t *testing.T) {
	rig := newOperatorRig(t)

	got, err := rig.ops.Messages(context.Background(), rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}

	m := got[0]
	if m.ID != rig.messageID {
		t.Errorf("id = %q, want %q", m.ID, rig.messageID)
	}
	if m.Total != 1 {
		t.Errorf("total = %d, want 1", m.Total)
	}
	if m.Failed != 0 {
		t.Errorf("failed = %d, want 0", m.Failed)
	}
	if len(m.Channels) != 1 || m.Channels[0] != "email" {
		t.Errorf("channels = %v, want [email]", m.Channels)
	}
}

// A customer reaching either read path is refused the same as at a write: a
// source's own message log is an operator's tool, not a customer's.
func TestACustomerCannotReadAnotherSourcesLog(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if _, err := rig.ops.Messages(ctx, rig.customer, rig.sourceID); err == nil {
		t.Error("a customer read the message log")
	} else if !errors.Is(err, usecase.ErrNotOperator) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotOperator", err)
	}

	if _, err := rig.ops.Deliveries(ctx, rig.customer, rig.sourceID, rig.messageID); err == nil {
		t.Error("a customer read a message's deliveries")
	} else if !errors.Is(err, usecase.ErrNotOperator) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotOperator", err)
	}
}

// Senders is what a source is configured to send as -- ordinary operator
// work, so an admin sees it without the super_admin check.
func TestSendersReturnsWhatTheSourceIsConfiguredToSendAs(t *testing.T) {
	rig := newOperatorRig(t)

	got, err := rig.ops.Senders(context.Background(), rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Senders: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d senders, want the one seeded by newOperatorRig", len(got))
	}
	if got[0].Name != "our-support-bot" {
		t.Errorf("name = %q", got[0].Name)
	}
}

// The audit log is not ordinary operator work, and the reason is the actor
// column: a customer is the actor of every source they register and every key
// they issue, so these rows are the roster. An admin is refused here for the
// same reason they are refused /people.
func TestOnlyASuperAdminReadsTheAuditLog(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	// One act by a customer, so what an admin would read is a customer's own
	// address rather than an operator's.
	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if _, err := rig.ops.Audit(ctx, rig.customer); err == nil {
		t.Error("a customer read the audit log")
	} else if !errors.Is(err, usecase.ErrNotOperator) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotOperator", err)
	}

	if _, err := rig.ops.Audit(ctx, rig.admin); err == nil {
		t.Error("an admin read the audit log")
	} else if !errors.Is(err, usecase.ErrNotSuperAdmin) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotSuperAdmin", err)
	}

	rows, err := rig.ops.Audit(ctx, rig.superAdmin)
	if err != nil {
		t.Fatalf("a super_admin could not read the audit log: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want the one Approve wrote", len(rows))
	}
}

// A customer reaching this is refused the same as Messages and Deliveries:
// what a source sends as is an operator's tool, approving it blind is the
// whole thing this method exists to prevent, and it is not a customer's own
// question to ask about somebody else's source.
func TestACustomerCannotReadSenders(t *testing.T) {
	rig := newOperatorRig(t)

	if _, err := rig.ops.Senders(context.Background(), rig.customer, rig.sourceID); err == nil {
		t.Error("a customer read the sender list")
	} else if !errors.Is(err, usecase.ErrNotOperator) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotOperator", err)
	}
}
