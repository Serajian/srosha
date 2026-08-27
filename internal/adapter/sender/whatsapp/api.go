package whatsapp

import "errors"

// maxResponseBytes bounds what is read back. An answer is a few hundred bytes;
// anything larger is not this API and should not be able to fill memory.
const maxResponseBytes = 1 << 16

var errNoMessageID = errors.New("whatsapp accepted the message without naming it")

// sendRequest is one message. The shape is theirs: type says which of the
// fields below is filled, and the others are absent rather than empty.
type sendRequest struct {
	MessagingProduct string `json:"messaging_product"`
	To               string `json:"to"`
	Type             string `json:"type"`

	Text     *textBody     `json:"text,omitempty"`
	Template *templateBody `json:"template,omitempty"`
}

type textBody struct {
	// PreviewURL off: a body that happens to contain a link should not turn into
	// a card nobody asked for.
	PreviewURL bool   `json:"preview_url"`
	Body       string `json:"body"`
}

type templateBody struct {
	Name       string           `json:"name"`
	Language   templateLanguage `json:"language"`
	Components []component      `json:"components,omitempty"`
}

type templateLanguage struct {
	Code string `json:"code"`
}

// component carries the positional parameters a template was approved with.
type component struct {
	Type       string      `json:"type"`
	Parameters []parameter `json:"parameters"`
}

type parameter struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// apiResponse is either the messages it accepted or the error it refused with.
type apiResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`

	Error *apiError `json:"error"`
}

// apiError is Meta's shape. code is the part worth reading: the message is
// English written for a person and changes without notice.
type apiError struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      int    `json:"code"`
	Subcode   int    `json:"error_subcode"`
	FBTraceID string `json:"fbtrace_id"`
}
