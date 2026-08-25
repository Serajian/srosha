package telegram

import "time"

// apiBase is where the Bot API lives. A constant rather than config: it is not
// an address anybody deploying this gets to choose, and one that could be
// pointed elsewhere is one that can be pointed at somebody collecting tokens.
const apiBase = "https://api.telegram.org"

// sendMethod is the one call this sender makes.
const sendMethod = "sendMessage"

// maxTextLen is the Bot API's own limit on one message.
//
// Counted in runes here and in UTF-16 code units there, which agree on
// everything except characters outside the basic plane -- an emoji counts once
// here and twice there. Close enough to catch the ordinary case before a
// network round trip; when it is wrong the API answers 400 and that is treated
// as permanent, which is the same conclusion by a slower road.
const maxTextLen = 4096

// titleGap separates a title from a body when both are present. Two newlines,
// because one reads as a wrapped line rather than a heading.
const titleGap = "\n\n"

// tokenAlphabet is every character a Bot API token may contain: an id, a colon
// and a secret. Anything else is refused, and that is not tidiness -- the token
// goes in the PATH of the url, so a value with a slash in it would be choosing
// which endpoint we call.
const tokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-:."

// defaultRetryAfter is what a 429 with no hint waits. The Bot API almost always
// sends parameters.retry_after; this is for the time it does not.
const defaultRetryAfter = 30 * time.Second
