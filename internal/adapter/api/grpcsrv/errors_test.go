package grpcsrv_test

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/api/grpcsrv"
	"github.com/Serajian/srosha/internal/adapter/auth"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusCarriesTheCodeTheTypeMeans(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"invalid input", errs.InvalidInputErr("x"), codes.InvalidArgument},
		{"unauthorized", errs.UnauthorizedErr("x"), codes.Unauthenticated},
		{"forbidden", errs.ForbiddenErr("x"), codes.PermissionDenied},
		{"not found", errs.NotFoundErr("x"), codes.NotFound},
		{"duplicate", errs.DuplicateErr("x"), codes.AlreadyExists},
		{"too many", errs.TooManyErr("x"), codes.ResourceExhausted},
		{"unavailable", errs.UnavailableErr("x"), codes.Unavailable},
		{"timeout", errs.TimeoutErr("x"), codes.DeadlineExceeded},
		{"internal", errs.InternalErr("x"), codes.Internal},
		{"not one of ours", errors.New("x"), codes.Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(grpcsrv.Status(tt.err)); got != tt.want {
				t.Errorf("code = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusPassesNilThrough(t *testing.T) {
	if err := grpcsrv.Status(nil); err != nil {
		t.Errorf("Status(nil) = %v, want nil", err)
	}
}

// The message is written for a client. The reason names columns, hosts,
// providers and the values that were rejected, and it exists so an operator can
// read the log -- not so a caller can read our internals.
func TestOnlyTheMessageCrossesTheWire(t *testing.T) {
	err := errs.NotFoundErr("notification not found").
		WithStr("source \"acme\": row 01J8XKQ2 in table notifications").
		WithErr(errors.New("dial tcp 10.0.0.5:5432: connection refused"))

	got := status.Convert(grpcsrv.Status(err)).Message()

	if got != "notification not found" {
		t.Errorf("message = %q, want just the client-facing one", got)
	}
	for _, leaked := range []string{"acme", "notifications", "10.0.0.5", "5432", "01J8XKQ2"} {
		if strings.Contains(got, leaked) {
			t.Errorf("the reason leaked %q into the message: %q", leaked, got)
		}
	}
}

// An error that is not one of ours has a text nobody wrote for a client. It
// could be a driver naming a host or a library naming a file, so none of it
// goes out.
func TestAForeignErrorSaysNothingAboutItself(t *testing.T) {
	err := errors.New("pq: password authentication failed for user \"srosha\"")

	got := status.Convert(grpcsrv.Status(err)).Message()

	if strings.Contains(got, "srosha") || strings.Contains(got, "password") {
		t.Errorf("a foreign error's text went out: %q", got)
	}
	if got == "" {
		t.Error("the caller was told nothing at all, not even that it failed")
	}
}

// Reflection is a streaming method, so the unary interceptors do not run on it.
// A server with it on tells anyone who reaches the port what it serves, with no
// key -- which is fine on a laptop and is not on a deployment.
func TestReflectionIsOffUnlessAskedFor(t *testing.T) {
	const service = "grpc.reflection.v1.ServerReflection"

	tests := []struct {
		name string
		on   bool
		want bool
	}{
		{"production", false, false},
		{"anywhere else", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := grpcsrv.New(deps(t, tt.on))
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			_, registered := server.GetServiceInfo()[service]
			if registered != tt.want {
				t.Errorf("reflection registered = %v, want %v", registered, tt.want)
			}

			// The rpcs themselves are there either way.
			for _, ours := range []string{
				"notification.v1.NotificationService",
				"notification.v1.WebhookService",
			} {
				if _, ok := server.GetServiceInfo()[ours]; !ok {
					t.Errorf("%s is not registered", ours)
				}
			}
		})
	}
}

// deps is the least that New accepts, which is all this test needs: it looks at
// what was registered, never at what a handler does.
func deps(t *testing.T, reflection bool) grpcsrv.Deps {
	t.Helper()

	notifications, err := grpcsrv.NewNotificationServer(&usecase.Submitter{}, &usecase.Querier{})
	if err != nil {
		t.Fatalf("NewNotificationServer: %v", err)
	}
	webhooks, err := grpcsrv.NewWebhookServer(&usecase.Registrar{})
	if err != nil {
		t.Fatalf("NewWebhookServer: %v", err)
	}

	return grpcsrv.Deps{
		Notifications: notifications,
		Webhooks:      webhooks,
		Authn:         &source.Authenticator{},
		Scheme:        auth.NewScheme(),
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Reflection:    reflection,
	}
}
