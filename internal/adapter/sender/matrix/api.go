package matrix

import "errors"

// maxResponseBytes bounds what is read back. An answer is a few hundred bytes;
// anything larger is not a homeserver and should not be able to fill memory.
const maxResponseBytes = 1 << 16

var errNoEventID = errors.New("matrix accepted the message without naming the event")

// sendRequest is one message event.
type sendRequest struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
}

// apiResponse is either the event it made or the error it refused with. Matrix
// puts both at the top level rather than nesting them.
type apiResponse struct {
	EventID string `json:"event_id"`

	// ErrCode is the machine-readable half -- M_FORBIDDEN, M_LIMIT_EXCEEDED --
	// and the part worth reading. Error is English for a person.
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`

	// RetryAfterMs comes with a rate limit, in milliseconds.
	RetryAfterMs int `json:"retry_after_ms"`
}
