package apns

import "errors"

// maxResponseBytes bounds what is read back. APNs answers a refusal with one
// short json object and a success with no body at all.
const maxResponseBytes = 1 << 13

var errNoNotificationID = errors.New("apns accepted the message without naming it")

// The headers APNs reads and answers with.
const (
	headerID       = "apns-id"
	headerTopic    = "apns-topic"
	headerPushType = "apns-push-type"
	headerPriority = "apns-priority"
)

// apiResponse is a refusal. There is no success shape: APNs answers 200 with an
// empty body and puts the id in a header.
type apiResponse struct {
	// Reason is the whole of it -- one word, and the only thing Apple says
	// about what went wrong.
	Reason string `json:"reason"`

	// Timestamp comes with Unregistered alone: when the device token stopped
	// being valid, in milliseconds. Kept because it is the one refusal that
	// says anything a source could act on beyond "stop sending here".
	Timestamp int64 `json:"timestamp"`
}
