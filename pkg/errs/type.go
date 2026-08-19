package errs

type Type int

const (
	ErrUnknown Type = iota

	ErrInvalidInput   // 400
	ErrUnauthorized   // 401
	ErrForbidden      // 403
	ErrNotFound       // 404
	ErrDuplicateEntry // 409
	ErrTooMany        // 429

	ErrInternal    // 500
	ErrUnavailable // 503
	ErrTimeout     // 504
)

func (t Type) String() string {
	switch t {
	case ErrUnknown:
		return "UNKNOWN"
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
	default:
		return "UNKNOWN"
	}
}

func (t Type) IsErrNotFound() bool {
	return t == ErrNotFound
}
