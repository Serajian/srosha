package matrix

import "time"

// sendPath is the one call this sender makes: put an event into a room, under a
// transaction id the caller chooses.
//
// The version is in the path and is a constant rather than config. Matrix keeps
// old versions working for a long time, so moving is a release -- which is the
// honest shape, because the version is this service's contract with every
// homeserver and not a per-source setting.
const sendPath = "/_matrix/client/v3/rooms/%s/send/m.room.message/%s"

// messageType is the event this sender writes. Plain text: a body with markup
// needs a second field and a format name, and nothing here has markup to send.
const messageType = "m.text"

// maxTextLen is a bound of our own, not the protocol's.
//
// Matrix has no documented limit on a message body, but a homeserver has an
// event size limit -- 64 KiB including everything around the body -- and a
// message that reaches it is refused after the round trip. This is comfortably
// under, and catches the ordinary mistake before the network.
const maxTextLen = 32 << 10

// titleGap separates a title from a body. Matrix has one text field, as the
// bot channels do.
const titleGap = "\n\n"

// defaultRetryAfter is what a rate limit with no stated wait uses. Matrix
// usually states one, in milliseconds.
const defaultRetryAfter = 5 * time.Second
