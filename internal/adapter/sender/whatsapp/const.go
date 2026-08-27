package whatsapp

import "time"

// apiBase and apiVersion are where the Cloud API lives.
//
// The version is a constant rather than config, and Meta retires versions on a
// schedule -- so moving is a release. That is the honest shape: the version is
// this service's contract with them, not a per-customer setting, and config
// would mean as many versions in production as there are sources.
const (
	apiBase    = "https://graph.facebook.com"
	apiVersion = "v21.0"
)

// sendPath is the one call this sender makes. The phone number id goes in it,
// which is why its characters are checked.
const sendPath = "messages"

// maxTextLen is the Cloud API's limit on a text body. Counted in runes here;
// when that is wrong the API answers 4xx, which is treated as final -- the same
// conclusion by a slower road.
const maxTextLen = 4096

// titleGap separates a title from a body. WhatsApp has one text field, as the
// bot channels do.
const titleGap = "\n\n"

// defaultRetryAfter is what a 429 with no stated wait uses.
const defaultRetryAfter = 30 * time.Second

// The metadata keys this channel reads.
//
// Metadata is the source's own and srosha defines nothing about it -- these are
// the names THIS adapter looks for, and no other channel is affected by them.
// A message carrying none of them is sent as text.
const (
	metaTemplate   = "template"
	metaLanguage   = "template_language"
	metaParameters = "template_params"
)

// defaultLanguage is what a template is sent in when the source names none.
const defaultLanguage = "en_US"

// idAlphabet is every character a phone number id may contain.
//
// It is digits in practice, but the check is what matters rather than the
// alphabet: the id goes in the PATH of the url, so a value with a slash in it
// would be choosing which endpoint we call. The token does not need this -- it
// travels in a header, which is the one real difference from the bot channels.
const idAlphabet = "0123456789"
