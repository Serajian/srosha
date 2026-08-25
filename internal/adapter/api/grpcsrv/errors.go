// Package grpcsrv is the gRPC face of this service: the generated servers, the
// interceptors around them, and the translation in both directions between
// protobuf and the core's own types.
//
// The name is grpcsrv rather than grpc so that nothing here has to alias
// google.golang.org/grpc, which every second file needs.
package grpcsrv

import (
	"github.com/Serajian/srosha/pkg/errs"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Status is the only way an error leaves this service.
//
// Two things cross the wire and no more: a code, and the message the core
// wrote for a client. The reason never does -- it names columns, hosts,
// providers and rejected values, and it exists so an operator can read the log,
// not so a caller can read our internals.
//
// An error that is not an AppError is reported as internal with a message of
// our own. Returning its text would leak whatever some library happened to say,
// and an unclassified error reaching this point is a bug on our side rather
// than something the caller can act on.
func Status(err error) error {
	if err == nil {
		return nil
	}

	appErr, ok := errs.As(err)
	if !ok {
		return status.Error(codes.Internal, "the request could not be completed")
	}
	return status.Error(code(appErr.Type()), appErr.Message())
}

// code is the whole of what this adapter knows about gRPC that the core does
// not. errs.Type carries no code on purpose: core is imported by both the gRPC
// server and everything else, and must not learn which protocol is in front.
func code(t errs.Type) codes.Code {
	switch t {
	case errs.ErrInvalidInput:
		return codes.InvalidArgument
	case errs.ErrUnauthorized:
		return codes.Unauthenticated
	case errs.ErrForbidden:
		return codes.PermissionDenied
	case errs.ErrNotFound:
		return codes.NotFound
	case errs.ErrDuplicateEntry:
		return codes.AlreadyExists
	case errs.ErrTooMany:
		return codes.ResourceExhausted
	case errs.ErrUnavailable:
		return codes.Unavailable
	case errs.ErrTimeout:
		return codes.DeadlineExceeded

	// Internal and Unknown both land here, and so does any type added later
	// that nobody remembered to map. Internal is the safe default: it tells the
	// caller nothing and it is not something they will retry into a loop.
	case errs.ErrInternal, errs.ErrUnknown:
		return codes.Internal
	default:
		return codes.Internal
	}
}
