package fcm

import "time"

// endpoint is the whole address. Unlike Matrix there is one host and it is
// Google's, so it is a constant here rather than a setting: a source brings a
// project, never a server.
//
// The version is in the path. v1 replaced the legacy server-key API, which
// Google has since shut off, and moving off it would be a release.
const endpoint = "https://fcm.googleapis.com/v1/projects/%s/messages:send"

// maxTitleLen and maxBodyLen are bounds of our own.
//
// FCM's real limit is on the whole message -- 4 KB of payload, data included --
// and it is refused after the round trip. These catch the ordinary mistake
// before the network without pretending to be the provider's rule.
const (
	maxTitleLen = 1 << 10
	maxBodyLen  = 2 << 10
)

// Device tokens have no documented shape: Google issues them, changes their
// length between versions, and promises nothing about their characters. So
// these are a sanity check for a pasted-wrong value and deliberately loose --
// a rule invented here would one day refuse a token that works.
const (
	minDeviceTokenLen = 32
	maxDeviceTokenLen = 4096
)

// defaultRetryAfter is what a rate limit with no stated wait uses. FCM usually
// sends Retry-After, and asks for a longer pause than most: its quotas are per
// project and a fast retry spends the same quota again.
const defaultRetryAfter = 30 * time.Second
