package webhook

import "errors"

// Sentinel errors of the webhook aggregate.
var (
	ErrMissingSource    = errors.New("source id is required")
	ErrEmptyURL         = errors.New("callback url is required")
	ErrMalformedURL     = errors.New("callback url cannot be parsed")
	ErrInsecureURL      = errors.New("callback url must use https")
	ErrPrivateURL       = errors.New("callback url points inside our own network")
	ErrCredentialsInURL = errors.New("callback url must not carry credentials")

	ErrBatchIntervalOutOfRange = errors.New("batch interval is out of range")
	ErrBatchSizeOutOfRange     = errors.New("batch size is out of range")
)
