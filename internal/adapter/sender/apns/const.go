package apns

import "time"

// The two APNs environments. They are separate services with separate device
// tokens: a token from a development build is unknown to production, and the
// answer is BadDeviceToken -- which reads as "this device" when the real
// mistake was the address of the service.
const (
	productionHost = "https://api.push.apple.com"
	sandboxHost    = "https://api.sandbox.push.apple.com"

	environmentProduction = "production"
	environmentSandbox    = "sandbox"
)

// sendPath carries the device token. That is why this channel checks the shape
// of an address and fcm does not: there the token is a value in a json body,
// here it is part of a url.
const sendPath = "/3/device/%s"

// pushTypeAlert is a notification a person sees. The other types -- background,
// voip, complication -- are the app talking to itself, which is not something
// this service has a message for.
const pushTypeAlert = "alert"

// priorityImmediate is "send it now". The alternative saves the device's
// battery by holding messages back, which is the wrong trade for a service
// whose whole purpose is that somebody finds out.
const priorityImmediate = "10"

// maxPayloadBytes is Apple's limit on the whole json, custom keys included.
// Checked before the call because the answer otherwise costs a round trip.
const maxPayloadBytes = 4096

// A device token is hexadecimal, and it goes in the path. Apple's have been 64
// characters for a long time without that being promised, so the length is a
// range and the alphabet is the real rule.
const (
	minDeviceTokenLen = 32
	maxDeviceTokenLen = 200
)

// defaultRetryAfter is what a rate limit uses. APNs sends no Retry-After, so
// this is entirely our own choice.
const defaultRetryAfter = 10 * time.Second
