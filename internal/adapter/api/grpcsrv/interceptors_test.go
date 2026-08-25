package grpcsrv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/auth"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var info = &grpc.UnaryServerInfo{FullMethod: "/notification.v1.NotificationService/Submit"}

// fakeKeys answers the way the statement does: no live key is a nil source, not
// an error.
type fakeKeys struct {
	src      *source.Source
	touchErr error
	touched  bool
}

func (f *fakeKeys) ReadSourceByKeyHash(
	_ context.Context, _ string, _ time.Time,
) (*source.Source, shared.ID, error) {
	if f.src == nil {
		return nil, "", nil
	}
	return f.src, shared.ID("01J8XKQ2R7M3NB4PZC5VD6K001"), nil
}

func (f *fakeKeys) Touch(_ context.Context, _ shared.ID, _ time.Time, _ time.Duration) error {
	f.touched = true
	return f.touchErr
}

func authenticator(keys *fakeKeys) *source.Authenticator {
	return source.NewAuthenticator(keys, time.Now, time.Hour)
}

func ctxWithKey(t *testing.T, key string) context.Context {
	t.Helper()
	return metadata.NewIncomingContext(
		t.Context(), metadata.Pairs(authHeader, "Bearer "+key),
	)
}

// --- bearer ------------------------------------------------------------------

// Everything it does not like comes back as the empty string, because the
// caller answers all of them the same way. Saying which one was wrong would
// describe our authentication to somebody probing it.
func TestBearerReadsOnlyWhatWeAccept(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"no metadata at all", t.Context(), ""},
		{
			"no authorization header",
			metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-other", "v")), "",
		},
		{
			"another scheme",
			metadata.NewIncomingContext(t.Context(), metadata.Pairs(authHeader, "Basic abc")), "",
		},
		{
			"no scheme",
			metadata.NewIncomingContext(t.Context(), metadata.Pairs(authHeader, "srosha_abc")), "",
		},
		{
			"two of them -- we cannot know which they meant",
			metadata.NewIncomingContext(t.Context(), metadata.MD{
				authHeader: []string{"Bearer one", "Bearer two"},
			}), "",
		},
		{
			"the scheme in any case, which rfc 7235 allows",
			metadata.NewIncomingContext(t.Context(), metadata.Pairs(authHeader, "bEaReR abc")),
			"abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bearer(tt.ctx); got != tt.want {
				t.Errorf("bearer() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- auth --------------------------------------------------------------------

func TestAuthPutsTheCallerIntoTheContext(t *testing.T) {
	scheme := auth.NewScheme()
	key, _, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	keys := &fakeKeys{src: &source.Source{ID: "acme", IsActive: true}}

	var seen *source.Source
	handler := func(ctx context.Context, _ any) (any, error) {
		seen, _ = SourceFrom(ctx)
		return "ok", nil
	}

	got, err := Auth(authenticator(keys), scheme, discard())(
		ctxWithKey(t, key), nil, info, handler,
	)
	if err != nil {
		t.Fatalf("Auth() error = %v", err)
	}
	if got != "ok" {
		t.Errorf("response = %v, want the handler's", got)
	}
	if seen == nil || seen.ID != "acme" {
		t.Errorf("the handler saw %v, want acme", seen)
	}
	if !keys.touched {
		t.Error("the key was not recorded as used")
	}
}

// A key of the wrong shape never reaches the database, and a key that is not
// ours is refused after it does. Both answer the same, or the difference tells
// somebody probing us which of their guesses had the right shape.
func TestAuthRefusesEverythingItCannotIdentify(t *testing.T) {
	scheme := auth.NewScheme()
	good, _, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	tests := []struct {
		name string
		ctx  context.Context
		keys *fakeKeys
	}{
		{"nothing presented", t.Context(), &fakeKeys{src: &source.Source{IsActive: true}}},
		{"a string of the wrong shape", ctxWithKey(t, "hello"), &fakeKeys{}},
		{"a well-shaped key nobody issued", ctxWithKey(t, good), &fakeKeys{}},
	}

	var messages []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := func(context.Context, any) (any, error) {
				called = true
				return nil, nil
			}

			_, err := Auth(authenticator(tt.keys), scheme, discard())(tt.ctx, nil, info, handler)
			if err == nil {
				t.Fatal("it was let through")
			}
			if called {
				t.Error("the handler ran anyway")
			}
			if !errors.Is(err, source.ErrUnknownKey) {
				t.Errorf("errors.Is(ErrUnknownKey) = false, got %v", err)
			}
			messages = append(messages, status.Convert(Status(err)).Message())
		})
	}

	for i := 1; i < len(messages); i++ {
		if messages[i] != messages[0] {
			t.Errorf("the answers differ:\n  %q\n  %q", messages[0], messages[i])
		}
	}
}

// A genuine key on a switched-off account is not a bad key. A customer told
// "invalid credentials" spends the outage rotating the wrong thing.
func TestASuspendedSourceIsForbiddenNotUnauthenticated(t *testing.T) {
	scheme := auth.NewScheme()
	key, _, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	keys := &fakeKeys{src: &source.Source{ID: "acme", IsActive: false}}
	handler := func(context.Context, any) (any, error) { return nil, nil }

	_, err = Auth(authenticator(keys), scheme, discard())(ctxWithKey(t, key), nil, info, handler)

	if !errors.Is(err, source.ErrSourceInactive) {
		t.Fatalf("error = %v, want ErrSourceInactive", err)
	}
	if got := status.Code(Status(err)); got != codes.PermissionDenied {
		t.Errorf("code = %v, want permission denied", got)
	}
}

// Recording that a key was used is bookkeeping. It must not be able to refuse a
// request that has already been authenticated.
func TestAFailedTouchDoesNotRefuseTheRequest(t *testing.T) {
	scheme := auth.NewScheme()
	key, _, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	keys := &fakeKeys{
		src:      &source.Source{ID: "acme", IsActive: true},
		touchErr: errors.New("write failed"),
	}
	handler := func(context.Context, any) (any, error) { return "ok", nil }

	got, err := Auth(authenticator(keys), scheme, discard())(
		ctxWithKey(t, key), nil, info, handler,
	)
	if err != nil {
		t.Fatalf("a failed touch refused the request: %v", err)
	}
	if got != "ok" {
		t.Errorf("response = %v, want the handler's", got)
	}
}

// --- recovery and errors -----------------------------------------------------

// A panic in one handler must not take the process with it, and the caller must
// not be handed our stack.
func TestRecoveryAnswersAPanicAndSaysNothing(t *testing.T) {
	handler := func(context.Context, any) (any, error) {
		panic("a nil map somewhere")
	}

	resp, err := Recovery(discard())(t.Context(), nil, info, handler)

	if resp != nil {
		t.Errorf("response = %v, want nothing", resp)
	}
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want internal", got)
	}
	if msg := status.Convert(err).Message(); msg == "a nil map somewhere" {
		t.Error("the panic's own text went to the caller")
	}
}

// Six handlers, and one of them will eventually forget to translate. Doing it
// here means forgetting is not possible.
func TestErrorsTranslatesWhatTheHandlerReturned(t *testing.T) {
	handler := func(context.Context, any) (any, error) {
		return nil, errs.NotFoundErr("notification not found").
			WithStr("row 01J8XKQ2 in table notifications")
	}

	_, err := Errors()(t.Context(), nil, info, handler)

	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want not found", got)
	}
	if msg := status.Convert(err).Message(); msg != "notification not found" {
		t.Errorf("message = %q, want only the client-facing one", msg)
	}
}

func TestErrorsLeavesASuccessAlone(t *testing.T) {
	handler := func(context.Context, any) (any, error) { return "ok", nil }

	got, err := Errors()(t.Context(), nil, info, handler)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got != "ok" {
		t.Errorf("response = %v, want the handler's", got)
	}
}
