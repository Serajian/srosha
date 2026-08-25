package bale

import "time"

// apiBase is where Bale's bot API lives. A constant rather than config, for the
// same reason Telegram's is: it is not an address anybody deploying this gets
// to choose, and one that could be pointed elsewhere is one that can be pointed
// at somebody collecting tokens.
const apiBase = "https://tapi.bale.ai"

// sendMethod is the one call this sender makes.
const sendMethod = "sendMessage"

// maxTextLen is the limit on one message.
//
// Counted in runes here. When it is wrong the API answers 400, which is treated
// as permanent -- the same conclusion by a slower road.
const maxTextLen = 4096

// titleGap separates a title from a body when both are present.
const titleGap = "\n\n"

// defaultRetryAfter is what a 429 with no stated wait uses.
const defaultRetryAfter = 30 * time.Second

// tokenAlphabet is every character a bot token may contain. Anything else is
// refused, and that is not tidiness -- the token goes in the PATH of the url,
// so a value with a slash in it would be choosing which endpoint we call.
const tokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-:."
