package delivery_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

var (
	notifID = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8G9H")
	now     = time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
	later   = now.Add(time.Minute)
)

// seqIDs hands out a fixed sequence, so a test can name the ids it expects.
func seqIDs() shared.IDFunc {
	n := 0
	return func() shared.ID {
		n++
		return shared.ID(fmt.Sprintf("01J8XQ2M4E7N9V3B5C6D7F8G%02d", n))
	}
}

func telegram(addr string) shared.Recipient {
	return shared.Recipient{Channel: shared.ChannelTelegram, Address: addr}
}

func email(addr string) shared.Recipient {
	return shared.Recipient{Channel: shared.ChannelEmail, Address: addr}
}

func newOne(t *testing.T) *delivery.Delivery {
	t.Helper()
	set, err := delivery.NewSet(
		notifID,
		[]shared.Recipient{telegram("123456789")},
		nil,
		seqIDs(),
		now,
	)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	return &set[0]
}

// The identity is stamped per channel, so two channels in one message can be
// sent from two different senders, and a channel with none falls back to the
// source's default.
func TestNewSetStampsTheSenderPerChannel(t *testing.T) {
	recipients := []shared.Recipient{
		email("ali@example.com"),
		telegram("123456789"),
	}
	senders := map[shared.Channel]string{shared.ChannelEmail: "marketing"}

	set, err := delivery.NewSet(notifID, recipients, senders, seqIDs(), now)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	if set[0].SenderName != "marketing" {
		t.Errorf("email SenderName = %q, want marketing", set[0].SenderName)
	}
	if set[1].SenderName != "" {
		t.Errorf("telegram SenderName = %q, want empty (the default)", set[1].SenderName)
	}
}

func TestNewSetLeavesTheSenderEmptyWhenNoneIsGiven(t *testing.T) {
	set, err := delivery.NewSet(
		notifID, []shared.Recipient{telegram("123456789")}, nil, seqIDs(), now)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if set[0].SenderName != "" {
		t.Errorf("SenderName = %q, want empty", set[0].SenderName)
	}
}

func TestNewSetOpensOneDeliveryPerRecipient(t *testing.T) {
	recipients := []shared.Recipient{
		telegram("123456789"),
		email("ali@example.com"),
		{Channel: shared.ChannelWhatsApp, Address: "+989121234567"},
	}

	set, err := delivery.NewSet(notifID, recipients, nil, seqIDs(), now)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if len(set) != len(recipients) {
		t.Fatalf("len = %d, want %d", len(set), len(recipients))
	}

	seen := map[shared.ID]bool{}
	for i, d := range set {
		if d.Recipient != recipients[i] {
			t.Errorf("[%d] recipient = %v, want %v", i, d.Recipient, recipients[i])
		}
		if d.NotificationID != notifID {
			t.Errorf("[%d] NotificationID = %q, want %q", i, d.NotificationID, notifID)
		}
		if d.Status() != delivery.StatusPending {
			t.Errorf("[%d] Status() = %v, want PENDING", i, d.Status())
		}
		if d.Attempts() != 0 {
			t.Errorf("[%d] Attempts() = %d, want 0", i, d.Attempts())
		}
		if !d.UpdatedAt().Equal(now) {
			t.Errorf("[%d] UpdatedAt() = %v, want %v", i, d.UpdatedAt(), now)
		}
		if d.ID.IsZero() || seen[d.ID] {
			t.Errorf("[%d] id %q is missing or repeated", i, d.ID)
		}
		seen[d.ID] = true
	}
}

// The same channel twice is what "one message to several people" means, and the
// same address on two channels is one person reachable in two ways. Only the
// whole recipient repeating is a mistake.
func TestNewSetAllowsRepeatsThatAreNotDuplicates(t *testing.T) {
	tests := []struct {
		name       string
		recipients []shared.Recipient
	}{
		{"same channel, different addresses", []shared.Recipient{
			telegram("111111111"), telegram("222222222"),
		}},
		{"same address, different channels", []shared.Recipient{
			{Channel: shared.ChannelTelegram, Address: "@acmenews"},
			{Channel: shared.ChannelBale, Address: "@acmenews"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := delivery.NewSet(notifID, tt.recipients, nil, seqIDs(), now)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			if len(set) != len(tt.recipients) {
				t.Errorf("len = %d, want %d", len(set), len(tt.recipients))
			}
		})
	}
}

func TestNewSetRejectsBadInput(t *testing.T) {
	tests := []struct {
		name       string
		notifID    shared.ID
		recipients []shared.Recipient
		nextID     shared.IDFunc
		now        time.Time
		sentinel   error
		typ        errs.Type
	}{
		{
			name: "no recipients", notifID: notifID, recipients: nil,
			nextID: seqIDs(), now: now,
			sentinel: delivery.ErrNoRecipients, typ: errs.ErrInvalidInput,
		},
		{
			name: "duplicate recipient", notifID: notifID,
			recipients: []shared.Recipient{telegram("123456789"), telegram("123456789")},
			nextID:     seqIDs(), now: now,
			sentinel: delivery.ErrDuplicateRecipient, typ: errs.ErrInvalidInput,
		},
		{
			name: "unknown channel", notifID: notifID,
			recipients: []shared.Recipient{{Channel: "carrier-pigeon", Address: "x"}},
			nextID:     seqIDs(), now: now,
			sentinel: shared.ErrUnknownChannel, typ: errs.ErrInvalidInput,
		},
		{
			name: "address shaped for another channel", notifID: notifID,
			recipients: []shared.Recipient{telegram("ali@example.com")},
			nextID:     seqIDs(), now: now,
			sentinel: shared.ErrInvalidAddress, typ: errs.ErrInvalidInput,
		},
		{
			name: "empty address", notifID: notifID,
			recipients: []shared.Recipient{telegram("")},
			nextID:     seqIDs(), now: now,
			sentinel: shared.ErrEmptyAddress, typ: errs.ErrInvalidInput,
		},
		{
			name: "missing notification id", notifID: "",
			recipients: []shared.Recipient{telegram("123456789")},
			nextID:     seqIDs(), now: now,
			sentinel: delivery.ErrMissingNotification, typ: errs.ErrInternal,
		},
		{
			name: "missing id generator", notifID: notifID,
			recipients: []shared.Recipient{telegram("123456789")},
			nextID:     nil, now: now,
			sentinel: delivery.ErrMissingIDFunc, typ: errs.ErrInternal,
		},
		{
			name: "generator returns nothing", notifID: notifID,
			recipients: []shared.Recipient{telegram("123456789")},
			nextID:     func() shared.ID { return "" }, now: now,
			sentinel: shared.ErrInvalidID, typ: errs.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := delivery.NewSet(tt.notifID, tt.recipients, nil, tt.nextID, tt.now)
			if err == nil {
				t.Fatalf("NewSet() = %+v, want an error", set)
			}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("errors.Is(%v) = false, want the sentinel", tt.sentinel)
			}
			if !errs.IsType(err, tt.typ) {
				t.Errorf("type = %v, want %v", errs.TypeOf(err), tt.typ)
			}
		})
	}
}

func TestMarkSent(t *testing.T) {
	d := newOne(t)

	if err := d.MarkSent("tg-8821", 2, later); err != nil {
		t.Fatalf("MarkSent() error = %v", err)
	}
	if d.Status() != delivery.StatusSent {
		t.Errorf("Status() = %v, want SENT", d.Status())
	}
	if d.ProviderMessageID() != "tg-8821" {
		t.Errorf("ProviderMessageID() = %q, want %q", d.ProviderMessageID(), "tg-8821")
	}
	if d.Attempts() != 2 {
		t.Errorf("Attempts() = %d, want 2", d.Attempts())
	}
	if !d.UpdatedAt().Equal(later) {
		t.Errorf("UpdatedAt() = %v, want %v", d.UpdatedAt(), later)
	}
	if !d.IsSettled() {
		t.Error("a sent delivery must be settled")
	}
}

func TestMarkFailed(t *testing.T) {
	d := newOne(t)

	if err := d.MarkFailed(delivery.FailurePermanent, "chat not found", 1, later); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	if d.Status() != delivery.StatusFailed {
		t.Errorf("Status() = %v, want FAILED", d.Status())
	}
	if d.FailureReason() != delivery.FailurePermanent {
		t.Errorf("FailureReason() = %v, want PERMANENT", d.FailureReason())
	}
	if d.LastError() != "chat not found" {
		t.Errorf("LastError() = %q, want %q", d.LastError(), "chat not found")
	}
	if d.Attempts() != 1 {
		t.Errorf("Attempts() = %d, want 1", d.Attempts())
	}
}

// A FAILED delivery that does not say why is exactly what the reason column
// exists to prevent, so the aggregate refuses to create one.
func TestMarkFailedNeedsAReason(t *testing.T) {
	d := newOne(t)

	err := d.MarkFailed("", "something", 1, later)
	if err == nil {
		t.Fatal("MarkFailed() with no reason should fail")
	}
	if !errors.Is(err, delivery.ErrMissingFailureReason) {
		t.Errorf("errors.Is(ErrMissingFailureReason) = false")
	}
	if !errs.IsType(err, errs.ErrInternal) {
		t.Errorf("type = %v, want internal", errs.TypeOf(err))
	}
	if d.Status() != delivery.StatusPending {
		t.Errorf("Status() = %v, want the delivery left untouched", d.Status())
	}
}

// This is the duplicate-send guard. A redelivered message finds the delivery
// already settled and is refused in memory, with no extra query.
func TestASettledDeliveryRefusesToMoveAgain(t *testing.T) {
	tests := []struct {
		name  string
		first func(*delivery.Delivery) error
		again func(*delivery.Delivery) error
	}{
		{
			"sent then sent",
			func(d *delivery.Delivery) error { return d.MarkSent("a", 1, later) },
			func(d *delivery.Delivery) error { return d.MarkSent("b", 2, later) },
		},
		{
			"sent then failed",
			func(d *delivery.Delivery) error { return d.MarkSent("a", 1, later) },
			func(d *delivery.Delivery) error {
				return d.MarkFailed(delivery.FailurePermanent, "late bounce", 2, later)
			},
		},
		{
			"failed then sent",
			func(d *delivery.Delivery) error {
				return d.MarkFailed(delivery.FailurePermanent, "gone", 1, later)
			},
			func(d *delivery.Delivery) error { return d.MarkSent("b", 2, later) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newOne(t)
			if err := tt.first(d); err != nil {
				t.Fatalf("first move error = %v", err)
			}
			before := d.Status()

			err := tt.again(d)
			if err == nil {
				t.Fatal("the second move should be refused")
			}
			if !errors.Is(err, delivery.ErrInvalidTransition) {
				t.Errorf("errors.Is(ErrInvalidTransition) = false")
			}
			if d.Status() != before {
				t.Errorf("Status() = %v, want it unchanged at %v", d.Status(), before)
			}
		})
	}
}

// MarkSent clears what an earlier failed attempt left behind, so a row can never
// read as SENT while still carrying a failure reason.
func TestMarkSentClearsAnEarlierFailure(t *testing.T) {
	d := delivery.Restore(delivery.Snapshot{
		ID:             shared.ID("01J8XQ2M4E7N9V3B5C6D7F8G01"),
		NotificationID: notifID,
		Recipient:      telegram("123456789"),
		SenderName:     "marketing",
		Status:         delivery.StatusPending,
		Attempts:       3,
		LastError:      "timeout",
		FailureReason:  delivery.FailurePermanent,
		UpdatedAt:      now,
	})

	if err := d.MarkSent("tg-1", 4, later); err != nil {
		t.Fatalf("MarkSent() error = %v", err)
	}
	if d.LastError() != "" {
		t.Errorf("LastError() = %q, want it cleared", d.LastError())
	}
	if d.FailureReason() != "" {
		t.Errorf("FailureReason() = %q, want it cleared", d.FailureReason())
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	notified := now.Add(time.Second)
	snap := delivery.Snapshot{
		ID:                shared.ID("01J8XQ2M4E7N9V3B5C6D7F8G01"),
		NotificationID:    notifID,
		Recipient:         email("ali@example.com"),
		SenderName:        "marketing",
		Status:            delivery.StatusFailed,
		Attempts:          5,
		LastError:         "mailbox full",
		FailureReason:     delivery.FailureMaxAttempts,
		ProviderMessageID: "smtp-99",
		NotifiedAt:        &notified,
		UpdatedAt:         later,
	}

	d := delivery.Restore(snap)

	if d.ID != snap.ID || d.NotificationID != snap.NotificationID || d.Recipient != snap.Recipient {
		t.Error("identity fields not carried through")
	}
	if d.SenderName != snap.SenderName {
		t.Errorf("SenderName = %q, want %q", d.SenderName, snap.SenderName)
	}
	if d.Status() != snap.Status || d.Attempts() != snap.Attempts {
		t.Error("state not carried through")
	}
	if d.LastError() != snap.LastError || d.FailureReason() != snap.FailureReason {
		t.Error("failure detail not carried through")
	}
	if d.ProviderMessageID() != snap.ProviderMessageID {
		t.Error("provider message id not carried through")
	}
	if d.NotifiedAt() == nil || !d.NotifiedAt().Equal(notified) {
		t.Error("notified_at not carried through")
	}
	if !d.UpdatedAt().Equal(snap.UpdatedAt) {
		t.Error("updated_at not carried through")
	}
}

// Restore must load a row the current rules would reject, so tightening a rule
// tomorrow cannot make yesterday's rows unreadable.
func TestRestoreLoadsARowNewSetWouldReject(t *testing.T) {
	d := delivery.Restore(delivery.Snapshot{
		ID:             shared.ID("01J8XQ2M4E7N9V3B5C6D7F8G01"),
		NotificationID: notifID,
		Recipient:      telegram("not-a-chat-id"), // NewSet refuses this
		Status:         delivery.StatusSent,
	})

	if d.Recipient.Address != "not-a-chat-id" {
		t.Errorf("Address = %q, want it loaded unchanged", d.Recipient.Address)
	}
}

func TestMarkNotified(t *testing.T) {
	d := newOne(t)
	if d.NotifiedAt() != nil {
		t.Fatal("a new delivery must not be marked notified")
	}

	d.MarkNotified(later)

	if d.NotifiedAt() == nil || !d.NotifiedAt().Equal(later) {
		t.Errorf("NotifiedAt() = %v, want %v", d.NotifiedAt(), later)
	}
}
