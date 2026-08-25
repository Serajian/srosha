package sender_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/sender"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	sourceID = "01K0SRC0000000000000000000"
	theirs   = "111:their-bot"
	ours     = "999:our-bot"
)

// --- stand-ins ---------------------------------------------------------------

type rows struct {
	byChannel map[shared.Channel][]credential.Credential
	err       error
}

func (r rows) ListBySourceAndChannel(
	_ context.Context, _ string, c shared.Channel,
) ([]credential.Credential, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.byChannel[c], nil
}

type secrets struct {
	secret string
	config []byte
	asked  int
	err    error
}

func (s *secrets) Material(
	context.Context, string, shared.Channel, shared.ID,
) ([]byte, string, error) {
	s.asked++
	if s.err != nil {
		return nil, "", s.err
	}
	return s.config, s.secret, nil
}

func cred(t *testing.T, name string, isDefault, isActive bool) credential.Credential {
	t.Helper()
	return credOn(t, shared.ChannelTelegram, name, isDefault, isActive)
}

func credOn(
	t *testing.T, ch shared.Channel, name string, isDefault, isActive bool,
) credential.Credential {
	t.Helper()

	c, err := credential.New(
		shared.ID("01K0CRED" + name + "00000000000000")[:26], sourceID,
		ch, name, isDefault, time.Now(),
	)
	if err != nil {
		t.Fatalf("credential.New(%q): %v", name, err)
	}
	if !isActive {
		c.Deactivate(time.Now())
	}
	return *c
}

func registry(t *testing.T, have []credential.Credential, s *secrets, own sender.Fallback) *sender.Registry {
	t.Helper()
	return registryOn(t, shared.ChannelTelegram, have, s, own)
}

func registryOn(
	t *testing.T, c shared.Channel, have []credential.Credential,
	s *secrets, own sender.Fallback,
) *sender.Registry {
	t.Helper()

	r, err := sender.NewRegistry(
		credential.NewService(rows{byChannel: map[shared.Channel][]credential.Credential{
			c: have,
		}}),
		s, http.DefaultClient, own,
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

// --- tests -------------------------------------------------------------------

func TestASourcesOwnIdentityIsUsed(t *testing.T) {
	s := &secrets{secret: theirs}
	r := registry(t, []credential.Credential{cred(t, "alerts", true, true)}, s,
		sender.Fallback{TelegramToken: ours})

	got, err := r.For(context.Background(), sourceID, shared.ChannelTelegram, "")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if got.Channel() != shared.ChannelTelegram {
		t.Errorf("Channel() = %q", got.Channel())
	}
	if s.asked != 1 {
		t.Errorf("the vault was asked %d times, want 1", s.asked)
	}
}

// The promise .env.dispatcher.example makes: ours is used when a source has not
// brought its own.
func TestOursStandsInWhenTheSourceRegisteredNothing(t *testing.T) {
	s := &secrets{}
	r := registry(t, nil, s, sender.Fallback{TelegramToken: ours})

	if _, err := r.For(context.Background(), sourceID, shared.ChannelTelegram, ""); err != nil {
		t.Fatalf("For: %v", err)
	}
	if s.asked != 0 {
		t.Error("a credential was opened for a source that has none")
	}
}

// And the line that promise stops at. A source that registered an identity and
// switched it off has chosen something; standing in would undo that silently.
func TestOursDoesNotStandInForAnIdentityThatWasSwitchedOff(t *testing.T) {
	r := registry(t, []credential.Credential{cred(t, "alerts", true, false)}, &secrets{},
		sender.Fallback{TelegramToken: ours})

	_, err := r.For(context.Background(), sourceID, shared.ChannelTelegram, "")
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Fatalf("For = %v, want invalid input so the core reports NO_SENDER", err)
	}
	if errors.Is(err, credential.ErrNoCredentials) {
		t.Error("a switched-off identity was reported as none registered")
	}
}

// Asking for a bot by name and getting srosha's would be worse than failing:
// the message goes out as somebody else and nobody finds out.
func TestANamedIdentityNeverFallsBack(t *testing.T) {
	r := registry(t, nil, &secrets{}, sender.Fallback{TelegramToken: ours})

	_, err := r.For(context.Background(), sourceID, shared.ChannelTelegram, "alerts")
	if !errors.Is(err, credential.ErrNotFound) {
		t.Errorf("For = %v, want not found", err)
	}
}

func TestNothingToStandInWithIsNoSender(t *testing.T) {
	r := registry(t, nil, &secrets{}, sender.Fallback{})

	_, err := r.For(context.Background(), sourceID, shared.ChannelTelegram, "")
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("For = %v, want invalid input", err)
	}
}

// Every channel that has a sender resolves through the same three answers.
func TestEveryWiredChannelResolves(t *testing.T) {
	tests := map[shared.Channel]sender.Fallback{
		shared.ChannelTelegram: {TelegramToken: ours},
		shared.ChannelBale:     {BaleToken: ours},
	}

	for c, own := range tests {
		t.Run(c.String(), func(t *testing.T) {
			// theirs
			s := &secrets{secret: theirs}
			got, err := registryOn(t, c, []credential.Credential{credOn(t, c, "alerts", true, true)}, s, own).
				For(context.Background(), sourceID, c, "")
			if err != nil {
				t.Fatalf("For: %v", err)
			}
			if got.Channel() != c {
				t.Errorf("Channel() = %q, want %q", got.Channel(), c)
			}

			// ours, when they registered nothing
			if _, err := registryOn(t, c, nil, &secrets{}, own).
				For(context.Background(), sourceID, c, ""); err != nil {
				t.Errorf("For with no credential = %v, want ours to stand in", err)
			}

			// and nothing to stand in with
			if _, err := registryOn(t, c, nil, &secrets{}, sender.Fallback{}).
				For(context.Background(), sourceID, c, ""); !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("For with no fallback = %v, want invalid input", err)
			}
		})
	}
}

// A channel with no sender written yet answers as configuration, not as a
// fault: the delivery is reported to the source and not retried.
func TestAChannelWithNoSenderYet(t *testing.T) {
	for _, c := range []shared.Channel{shared.ChannelEmail, shared.ChannelWhatsApp} {
		t.Run(c.String(), func(t *testing.T) {
			r := registry(t, nil, &secrets{}, sender.Fallback{TelegramToken: ours})

			_, err := r.For(context.Background(), sourceID, c, "")
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("For(%q) = %v, want invalid input", c, err)
			}
		})
	}
}

// A vault that cannot answer is not a configuration problem, and must not be
// reported as one: NO_SENDER is written down and never tried again.
func TestAVaultFailureIsNotNoSender(t *testing.T) {
	s := &secrets{err: errs.UnavailableErr("the request could not be completed")}
	r := registry(t, []credential.Credential{cred(t, "alerts", true, true)}, s,
		sender.Fallback{TelegramToken: ours})

	_, err := r.For(context.Background(), sourceID, shared.ChannelTelegram, "")
	if errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("For = %v, want it to stay retryable", err)
	}
}

func TestARegistryRefusesToBeBuiltHalfWired(t *testing.T) {
	creds := credential.NewService(rows{})

	if _, err := sender.NewRegistry(nil, &secrets{}, http.DefaultClient, sender.Fallback{}); err == nil {
		t.Error("NewRegistry with no credentials succeeded")
	}
	if _, err := sender.NewRegistry(creds, nil, http.DefaultClient, sender.Fallback{}); err == nil {
		t.Error("NewRegistry with no vault succeeded")
	}
	if _, err := sender.NewRegistry(creds, &secrets{}, nil, sender.Fallback{}); err == nil {
		t.Error("NewRegistry with no client succeeded")
	}
}
