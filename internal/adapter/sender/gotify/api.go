package gotify

import "errors"

// maxResponseBytes bounds what is read back. An answer is a few hundred bytes;
// anything larger is not this API and should not be able to fill memory.
const maxResponseBytes = 1 << 16

var errNoMessageID = errors.New("gotify accepted the message without naming it")

// sendRequest is one message. Title and Message are Gotify's own field names,
// documented in its Client-Server API -- unlike the bot channels and Matrix,
// Gotify has a title field of its own, so a title and a body never have to be
// merged into one string.
type sendRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`

	// Extras is how Gotify is told the message is not plain text, and it is
	// omitted entirely when it is: an absent key means the client's default,
	// while an empty object is a shape the API has no reason to accept.
	Extras *extras `json:"extras,omitempty"`
}

// extras carries the one key this service sets. Gotify's extras are an open
// map that clients and plugins read; `client::display` is the documented one
// for how a client should render a message.
//
// A named type rather than map[string]any because the shape is fixed and known
// -- a map would let a typo compile.
type extras struct {
	Display display `json:"client::display"`
}

type display struct {
	ContentType string `json:"contentType"`
}

// apiResponse is the message Gotify made, on success. Documented as returning
// the row it stored, of which only the id matters here.
type apiResponse struct {
	ID int64 `json:"id"`
}

// apiError is ASSUMED to be Gotify's error shape -- unverified, this service
// has no network access to check it against a real server. It follows the
// convention used elsewhere in Gotify's own tooling: a short machine name, a
// numeric code and an English description. If the assumption is wrong, the
// fields simply decode as zero values and Detail falls back to the http
// status alone in classify, which is still enough to decide on a retry.
type apiError struct {
	Error            string `json:"error"`
	ErrorCode        int    `json:"errorCode"`
	ErrorDescription string `json:"errorDescription"`
}
