package grpcsrv

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// The interceptors are one package with the handlers rather than a package of
// their own. They share the context key the authenticated source travels in and
// the error translation, and splitting them would mean either an import cycle
// or one of those living somewhere it does not belong.
//
// Order is fixed, outermost first:
//
//	Recovery → Logging → Errors → Auth → handler
//
// Recovery is outermost or a panic inside any other one takes the process with
// it. Logging is next so it sees the code that actually went back, which means
// Errors has to be inside it. Auth is last because everything above it is the
// same for a request that never gets to be authenticated.
//
// There is no rate limit interceptor, on purpose. The quota is spent by
// source.Service.Admit, on the sending path and nowhere else -- managing a
// webhook must not cost a message. An interceptor would charge every rpc and
// charge Submit twice.

// Recovery turns a panic into an answer instead of a dead process.
//
// The stack goes to the log and never to the caller: it names our packages, our
// paths and whatever was in scope. The caller is told what any internal failure
// is told, which is almost nothing.
func Recovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(ctx, "panic in handler",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(debug.Stack()),
				)
				resp = nil
				err = status.Error(codes.Internal, "the request could not be completed")
			}
		}()
		return handler(ctx, req)
	}
}

// Logging records one line per call: which method, how long, and how it ended.
//
// The error's own text is logged in full -- this is the one place the reason is
// meant to be read, and it is the reason the message deliberately does not
// carry what an operator needs.
func Logging(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {
		started := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err)

		attrs := []any{
			"method", info.FullMethod,
			"code", code.String(),
			"took", time.Since(started),
		}

		switch {
		case err == nil:
			log.InfoContext(ctx, "handled", attrs...)
		case code == codes.Internal, code == codes.Unavailable, code == codes.Unknown:
			// Ours to fix. Everything else is the caller being told no, which
			// is the service working.
			log.ErrorContext(ctx, "handled", append(attrs, "err", err)...)
		default:
			log.InfoContext(ctx, "handled", append(attrs, "err", err)...)
		}
		return resp, err
	}
}

// Errors is where every error becomes something a client may see.
//
// It is an interceptor rather than a call at the end of each handler because
// there are six handlers and one of them will eventually forget. Forgetting
// means returning a raw error, whose text can name a column, a host or the
// value that was rejected.
func Errors() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, Status(err)
		}
		return resp, nil
	}
}

// sourceKey is unexported and its own type, so nothing outside this package can
// put a source into a context or take one out by accident. A handler that
// could be handed a source by its caller would be a handler whose identity
// check can be bypassed.
type sourceKey struct{}

// withSource is the only way a source gets into a context, and it is
// unexported: a handler that could be handed one by its caller would be a
// handler whose identity check can be bypassed.
func withSource(ctx context.Context, src *source.Source) context.Context {
	return context.WithValue(ctx, sourceKey{}, src)
}

// SourceFrom is how a handler learns who is calling.
//
// It reports failure rather than returning nil, and every handler checks: an
// rpc reachable without the interceptor in front of it would run with no
// identity at all, and that is worth an error rather than a nil dereference.
func SourceFrom(ctx context.Context) (*source.Source, bool) {
	src, ok := ctx.Value(sourceKey{}).(*source.Source)
	return src, ok
}

// KeyScheme turns the key a caller presented into what the core looks it up by.
//
// It is declared here rather than imported from the adapter that implements it.
// This package would otherwise reach sideways into another adapter, which
// nothing else in this repository does -- and the rule that keeps that true is
// that an interface belongs to whoever calls it, not to whoever satisfies it.
//
// What a key looks like and how it is hashed stays entirely on the other side
// of this line. All that is said here is that one string becomes another.
type KeyScheme interface {
	// Parse refuses a string that is not shaped like one of our keys, with
	// exactly the answer an unknown key gets.
	Parse(presented string) (string, error)
}

// Auth resolves the caller from the key they presented, and refuses everything
// else. There is no list of public methods: health is served over http, so
// every rpc this service answers is for a source we have identified.
//
// The source goes into the context rather than being handed to the handler,
// because the handler signature is generated and cannot carry it.
func Auth(
	authn *source.Authenticator, scheme KeyScheme, log *slog.Logger,
) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {
		// Parse checks the shape before anything touches the database, and
		// refuses a badly shaped key with exactly the answer an unknown one
		// gets. Two different answers would let somebody learn our format.
		hash, err := scheme.Parse(bearer(ctx))
		if err != nil {
			return nil, err
		}

		src, keyID, err := authn.Authenticate(ctx, hash)
		if err != nil {
			return nil, err
		}

		// Bookkeeping, and it must not be able to refuse a request that has
		// already been authenticated. The only right answer to a failure here
		// is to write it down and carry on.
		if err := authn.RecordUse(ctx, keyID); err != nil {
			log.WarnContext(ctx, "could not record key use", "key_id", keyID, "err", err)
		}

		return handler(withSource(ctx, src), req)
	}
}

// bearer pulls the presented key out of the request metadata.
//
// It returns the empty string for everything it does not like -- no metadata,
// no header, the wrong scheme -- because the caller answers all of those the
// same way. Saying which one was wrong would describe our authentication to
// somebody probing it.
func bearer(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := md.Get(authHeader)
	if len(values) != 1 {
		return ""
	}

	presented := values[0]
	if len(presented) < len(bearerPrefix) ||
		!strings.EqualFold(presented[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}
	return presented[len(bearerPrefix):]
}
