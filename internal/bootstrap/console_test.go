package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/sender"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/infra/appleauth"
	"github.com/Serajian/srosha/internal/infra/googleauth"
	"github.com/Serajian/srosha/internal/infra/smtp"
)

// The console's registry has no fallback identity, and that is the whole
// security boundary of the trial feature.
//
// With one, a customer whose own credential is broken would press "test",
// srosha would send as itself, and the page would say it worked. A wrong answer
// that looks exactly like a right one.
//
// This calls consoleSenders -- the function the binary itself calls -- rather
// than rebuilding a registry beside it. A copy would still pass on the day
// somebody fills the fallback in, which is the one day it has to fail.
func TestTheConsoleCannotSendAsSrosha(t *testing.T) {
	reg, err := consoleSenders(
		credential.NewService(noCredentials{}, time.Now),
		noSecrets{}, &http.Client{}, noDialer{}, noGoogle{}, noApple{},
	)
	if err != nil {
		t.Fatalf("build the console registry: %v", err)
	}

	for _, c := range []shared.Channel{
		shared.ChannelEmail, shared.ChannelTelegram, shared.ChannelBale,
		shared.ChannelWhatsApp, shared.ChannelMatrix, shared.ChannelGotify,
		shared.ChannelFCM, shared.ChannelAPNs,
	} {
		t.Run(string(c), func(t *testing.T) {
			// No credential of its own and no name asked for is the one path
			// that reaches the fallback. It must not reach a sender.
			_, err := reg.For(context.Background(), "01K0SRC00000000000000000AB", c, "")
			if err == nil {
				t.Fatalf("the console built a %s sender with no credential: "+
					"it can send as srosha", c)
			}
			if !errors.Is(err, sender.ErrNoSender) {
				t.Errorf("refused %s for the wrong reason: %v", c, err)
			}
		})
	}
}

// --- stubs -------------------------------------------------------------------
//
// A source that registered nothing. Nothing below is ever reached: every
// channel is refused before a client, a key or a connection is wanted.

type noCredentials struct{}

func (noCredentials) ListBySourceAndChannel(
	context.Context, string, shared.Channel,
) ([]credential.Credential, error) {
	return nil, nil
}

func (noCredentials) ListBySourceID(
	context.Context, string,
) ([]credential.Credential, error) {
	return nil, nil
}

func (noCredentials) ReadByID(
	context.Context, string, shared.ID,
) (*credential.Credential, error) {
	return nil, nil
}

func (noCredentials) Deactivate(context.Context, *credential.Credential) error { return nil }
func (noCredentials) Activate(context.Context, *credential.Credential) error   { return nil }
func (noCredentials) SetDefault(context.Context, *credential.Credential) error { return nil }

func (noCredentials) ClearDefault(
	context.Context, string, shared.Channel, time.Time,
) error {
	return nil
}

type noSecrets struct{}

func (noSecrets) Material(
	context.Context, string, shared.Channel, shared.ID,
) ([]byte, string, error) {
	return nil, "", errors.New("no credential to open")
}

type noDialer struct{}

func (noDialer) Open(smtp.Identity) (*smtp.Client, error) {
	return nil, errors.New("the console must not dial for a trial")
}

type noGoogle struct{}

func (noGoogle) Open([]byte) (*googleauth.Source, error) {
	return nil, errors.New("no service account")
}

type noApple struct{}

func (noApple) Open([]byte, appleauth.Identity) (*appleauth.Source, error) {
	return nil, errors.New("no signing key")
}
