package telegram

import "errors"

// maxResponseBytes bounds what is read back. An answer is a few hundred bytes;
// anything larger is not the Bot API and should not be able to fill memory.
const maxResponseBytes = 1 << 16

var errNoResult = errors.New("telegram said ok with no result")

type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`

	// omitempty matters: an empty parse_mode field is rejected, while an absent
	// one means plain text.
	ParseMode string `json:"parse_mode,omitempty"`
}

// apiResponse is the envelope every Bot API method answers in. ok is the field
// that matters -- the http status agrees with it, but ok is what the API itself
// treats as the answer.
type apiResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`

	Result     *sendMessageResult `json:"result"`
	Parameters *responseParams    `json:"parameters"`
}

type sendMessageResult struct {
	MessageID int64 `json:"message_id"`
}

type responseParams struct {
	// RetryAfter is in seconds, and is the only wait the API ever states.
	RetryAfter int `json:"retry_after"`
}
