package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

// recordingRegistry keeps what it was asked for. fakeRegistry answers but does
// not remember, and what a trial resolves by is the thing under test here.
type recordingRegistry struct {
	sender *fakeSender
	err    error

	channel shared.Channel
	name    string
	asked   int
}

func (r *recordingRegistry) For(
	_ context.Context, _ string, c shared.Channel, name string,
) (delivery.Sender, error) {
	r.channel, r.name, r.asked = c, name, r.asked+1
	if r.err != nil {
		return nil, r.err
	}
	return r.sender, nil
}

type trialRig struct {
	trials *usecase.Trials
	reg    *recordingRegistry
	out    *fakeSender
	log    *auditLog
	actor  *user.User
	src    *source.Source
	cred   credential.Credential
}

// newTrialRig is one approved source with an email default address and one
// email identity registered on it -- the state a customer is in when the
// button is worth pressing.
func newTrialRig(t *testing.T, approved, allow bool) *trialRig {
	t.Helper()

	at := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	actor, err := user.New(
		shared.ID("01K0ACCT0000000000000000AB"), "me@acme.test", user.RoleCustomer, at,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}

	src, err := source.New(
		"01J8XQ2M4E7N9V3B5C6D7F8SRC", actor.ID, "acme-billing",
		map[shared.Channel]string{shared.ChannelEmail: "billing@acme.test"}, at,
	)
	if err != nil {
		t.Fatalf("source.New: %v", err)
	}
	if approved {
		if err := src.Approve(at); err != nil {
			t.Fatalf("Approve: %v", err)
		}
	}

	cred, err := credential.New(
		shared.ID("01K0CRED0000000000000000AB"), src.ID,
		shared.ChannelEmail, "primary", true, at,
	)
	if err != nil {
		t.Fatalf("credential.New: %v", err)
	}

	rows := newFakeCredentials(map[shared.Channel][]credential.Credential{
		shared.ChannelEmail: {*cred},
	})
	log := &auditLog{}
	reg := &recordingRegistry{
		sender: &fakeSender{channel: shared.ChannelEmail, providerID: "prov-42"},
	}

	return &trialRig{
		trials: usecase.NewTrials(
			source.NewService(
				fakeSources{byID: map[string]*source.Source{src.ID: src}},
				fakeLimiter{allow: true},
			),
			credential.NewService(rows, fixedNow(at)),
			reg,
			usecase.NewGate(log, nil, seqIDs(), fixedNow(at)),
			fakeLimiter{allow: allow},
			seqIDs(),
		),
		reg: reg, out: reg.sender, log: log, actor: actor, src: src, cred: *cred,
	}
}

func (r *trialRig) run(t *testing.T) (string, error) {
	t.Helper()
	return r.trials.Run(context.Background(), r.actor, r.src.ID, r.cred.ID)
}

// A source waiting for review has not been let out yet. A trial would be srosha
// sending on behalf of something it has not approved.
func TestATrialNeedsAnActiveSource(t *testing.T) {
	rig := newTrialRig(t, false, true)

	if _, err := rig.run(t); !errors.Is(err, source.ErrSourceInactive) {
		t.Fatalf("an unapproved source was allowed to send a test: %v", err)
	}
	if rig.out.count() != 0 {
		t.Error("it sent anyway")
	}
}

// The address is the channel's default and nothing else. Without one the trial
// has nowhere to go, and the refusal names the channel that is missing it.
func TestATrialNeedsADefaultAddressForThatChannel(t *testing.T) {
	rig := newTrialRig(t, true, true)

	// The source's default is on email; this identity is on a channel it has
	// no address for.
	other, err := credential.New(
		shared.ID("01K0CRED0000000000000000AC"), rig.src.ID,
		shared.ChannelTelegram, "bot", true, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("credential.New: %v", err)
	}
	rig.cred = *other

	rows := newFakeCredentials(map[shared.Channel][]credential.Credential{
		shared.ChannelTelegram: {*other},
	})
	rig.trials = usecase.NewTrials(
		source.NewService(
			fakeSources{byID: map[string]*source.Source{rig.src.ID: rig.src}},
			fakeLimiter{allow: true},
		),
		credential.NewService(rows, time.Now().UTC),
		rig.reg,
		usecase.NewGate(rig.log, nil, seqIDs(), time.Now().UTC),
		fakeLimiter{allow: true},
		seqIDs(),
	)

	_, err = rig.run(t)
	if !errors.Is(err, usecase.ErrTrialNoAddress) {
		t.Fatalf("a channel with no default address was allowed: %v", err)
	}
	if !strings.Contains(err.Error(), string(shared.ChannelTelegram)) {
		t.Errorf("the refusal does not name the channel: %v", err)
	}
}

// Whether a credential id exists is not a customer's business, so a stranger's
// id is not found rather than forbidden.
func TestAStrangersCredentialIsNotFound(t *testing.T) {
	rig := newTrialRig(t, true, true)
	rig.cred.ID = shared.ID("01K0CRED000000000000000ZZZ")

	if _, err := rig.run(t); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("a stranger's credential id gave %v, want not-found", err)
	}
	if rig.reg.asked != 0 {
		t.Error("it went looking for a sender for somebody else's identity")
	}
}

// The registry is asked for the credential's NAME, not its id. A trial that
// resolved differently from a real send would prove something else -- and an
// identity the source switched off has to be refused here exactly as it is in
// production.
func TestATrialResolvesByNameLikeARealSend(t *testing.T) {
	rig := newTrialRig(t, true, true)

	got, err := rig.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "prov-42" {
		t.Errorf("provider id = %q, want the provider's own", got)
	}
	if rig.reg.name != rig.cred.Name {
		t.Errorf("resolved by %q, want the name %q", rig.reg.name, rig.cred.Name)
	}
	if rig.reg.channel != rig.cred.Channel {
		t.Errorf("resolved on %q, want %q", rig.reg.channel, rig.cred.Channel)
	}

	sent := rig.out.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want one", len(sent))
	}
	if sent[0].Recipient.Address != "billing@acme.test" {
		t.Errorf("it went to %q, not the channel's default", sent[0].Recipient.Address)
	}
	if sent[0].Body == "" {
		t.Error("the test message has no body, so nobody receiving it knows what it is")
	}
}

// The provider's own words reach the caller. "401 Unauthorized" is something a
// customer can act on; "test failed" is not.
func TestTheProvidersErrorSurvives(t *testing.T) {
	rig := newTrialRig(t, true, true)
	refused := errors.New("telegram: 401 Unauthorized")
	rig.out.err = refused

	_, err := rig.run(t)
	if !errors.Is(err, refused) {
		t.Fatalf("the provider's error did not survive: %v", err)
	}
}

// The button really sends, so without a cap it is a way to make srosha's server
// send whatever somebody wants as fast as they can click.
func TestATrialOverTheCapIsRefused(t *testing.T) {
	rig := newTrialRig(t, true, false)

	if _, err := rig.run(t); !errors.Is(err, usecase.ErrTrialTooMany) {
		t.Fatalf("the cap did not refuse: %v", err)
	}
	if rig.out.count() != 0 {
		t.Error("it sent past the cap")
	}
	if rig.reg.asked != 0 {
		t.Error("it built a sender it was never going to use")
	}
}

// A trial writes one audit row naming the customer, the same shape as
// key.issue -- a customer's own action on their own source.
func TestATrialIsAudited(t *testing.T) {
	rig := newTrialRig(t, true, true)

	if _, err := rig.run(t); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.log.entries) != 1 {
		t.Fatalf("wrote %d audit rows, want one", len(rig.log.entries))
	}

	row := rig.log.entries[0]
	if row.Verb != usecase.ActCredentialTest {
		t.Errorf("verb = %q", row.Verb)
	}
	if row.TargetID != rig.cred.ID.String() {
		t.Errorf("target = %q, want the credential", row.TargetID)
	}
	if row.ActorEmail != rig.actor.Email {
		t.Errorf("actor = %q, want the customer", row.ActorEmail)
	}
}
