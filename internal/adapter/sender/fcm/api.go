package fcm

import "errors"

// maxResponseBytes bounds what is read back. An answer is a few hundred bytes;
// an error with details is a few thousand.
const maxResponseBytes = 1 << 16

var errNoMessageName = errors.New("fcm accepted the message without naming it")

// sendRequest is one message to one device.
//
// Only the token form is used. FCM can also address a topic or a condition,
// which are broadcasts -- and a broadcast has no recipient, so there is nothing
// in this service to record it as.
type sendRequest struct {
	Message message `json:"message"`
}

type message struct {
	Token        string            `json:"token"`
	Notification notification      `json:"notification"`
	Data         map[string]string `json:"data,omitempty"`
}

type notification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body"`
}

// apiResponse is either the message it made or the error it refused with.
type apiResponse struct {
	// Name is FCM's id, and it is a path: projects/x/messages/y.
	Name string `json:"name"`

	Error apiError `json:"error"`
}

// apiError is Google's standard error envelope, which every one of their APIs
// answers with. The part specific to FCM is inside Details.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`

	// Status is the coarse name -- NOT_FOUND, UNAUTHENTICATED. Present on every
	// Google error, and the fallback when Details says nothing.
	Status string `json:"status"`

	Details []apiErrorDetail `json:"details"`
}

type apiErrorDetail struct {
	Type string `json:"@type"`

	// ErrorCode is the one worth reading: UNREGISTERED, SENDER_ID_MISMATCH.
	// Present only on the FcmError detail, which is why Type is kept.
	ErrorCode string `json:"errorCode"`
}

// fcmCode finds the FCM-specific code among the details, if there is one.
func (e apiError) fcmCode() string {
	for _, d := range e.Details {
		if d.ErrorCode != "" {
			return d.ErrorCode
		}
	}
	return ""
}
