package webhook

import "errors"

// Sentinel errors of the webhook aggregate.
var (
	ErrNotFound = errors.New("no callback registered for this source")

	ErrMissingSource    = errors.New("source id is required")
	ErrEmptyURL         = errors.New("callback url is required")
	ErrMalformedURL     = errors.New("callback url cannot be parsed")
	ErrInsecureURL      = errors.New("callback url must use https")
	ErrPrivateURL       = errors.New("callback url points inside our own network")
	ErrCredentialsInURL = errors.New("callback url must not carry credentials")
)
