package bale

import "errors"

// maxResponseBytes bounds what is read back. An answer is a few hundred bytes;
// anything larger is not this API and should not be able to fill memory.
const maxResponseBytes = 1 << 16

var errNoResult = errors.New("bale said ok with no result")

type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`

	// omitempty matters: an empty parse_mode field is rejected, while an absent
	// one means plain text.
	ParseMode string `json:"parse_mode,omitempty"`
}

// apiResponse is the envelope every method answers in. ok is the field that
// matters -- the http status agrees with it, but ok is what the API itself
// treats as the answer.
type apiResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`

	Result *sendMessageResult `json:"result"`

	// Parameters is read when it is there and not relied on when it is not:
	// a 429 without one falls back to a wait of our own.
	Parameters *responseParams `json:"parameters"`
}

type sendMessageResult struct {
	MessageID int64 `json:"message_id"`
}

type responseParams struct {
	RetryAfter int `json:"retry_after"`
}
