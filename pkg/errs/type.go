package errs

// Type is the transport-neutral classification of an error.
//
// It carries no HTTP status and no gRPC code on purpose: this package is
// imported by internal/core, and core must not know which protocol is in
// front of it. Each adapter owns its own mapping -- see
// adapter/api/grpc/errors.go for Type -> codes.Code.
type Type int

const (
	ErrUnknown Type = iota

	ErrInvalidInput
	ErrUnauthorized
	ErrForbidden
	ErrNotFound
	ErrDuplicateEntry
	ErrTooMany

	ErrInternal
	ErrUnavailable
	ErrTimeout
)

func (t Type) String() string {
	switch t {
	case ErrInvalidInput:
		return "INVALID_INPUT"
	case ErrUnauthorized:
		return "UNAUTHORIZED"
	case ErrForbidden:
		return "FORBIDDEN"
	case ErrNotFound:
		return "NOT_FOUND"
	case ErrDuplicateEntry:
		return "DUPLICATE_ENTRY"
	case ErrTooMany:
		return "TOO_MANY_REQUESTS"
	case ErrInternal:
		return "INTERNAL_ERROR"
	case ErrUnavailable:
		return "SERVICE_UNAVAILABLE"
	case ErrTimeout:
		return "TIMEOUT"
	case ErrUnknown:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}
