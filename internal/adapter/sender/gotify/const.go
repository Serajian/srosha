package gotify

import "time"

// sendPath is the one call this sender makes: post a message.
const sendPath = "/message"

// tokenParam is Gotify's own documented query parameter for the application
// token. appIDParam is one this service adds -- see (*Sender).endpoint for
// why, and what it assumes.
const (
	tokenParam = "token"
	appIDParam = "appid"
)

// maxTextLen is a bound of our own, not the protocol's.
//
// Gotify documents no limit on a message body -- it is a self-hosted server
// storing rows in its own database. This is comfortably generous and catches
// the ordinary mistake before the round trip rather than after it.
const maxTextLen = 32 << 10

// defaultRetryAfter is what a 429 with no stated wait uses. Gotify does not
// document rate limiting at all; this is the same fallback the bot channels
// use for the same undocumented case.
const defaultRetryAfter = 30 * time.Second
