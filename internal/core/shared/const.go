package shared

const (
	idLength      = 26
	crockfordBase = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // no I, L, O, U

	// Telegram's own limits for an @name, which Bale follows.
	minUsernameLen = 5
	maxUsernameLen = 32

	// Page bounds. The ceiling is hard, so one request cannot pull a whole
	// table into memory however large a limit it asks for.
	DefaultPageSize = 50
	MaxPageSize     = 500

	// E.164 allows 8 to 15 digits after the "+".
	minE164Digits = 8
	maxE164Digits = 15
)

// matrixRoomSigil is what a room id begins with. Matrix puts the kind of a
// thing in its first character: "!" a room, "@" a user, "#" an alias. Only the
// first is an address this service can send to.
const matrixRoomSigil = "!"

// Bounds for an FCM device token. Loose on purpose: Google documents no shape
// for them, so this catches a value pasted wrong and nothing more.
const (
	minDeviceTokenLen = 32
	maxDeviceTokenLen = 4096
)

// Bounds for an APNs device token. Apple's have been 64 hexadecimal characters
// for a long time without that being promised, so the alphabet is the rule and
// the length is a range.
const (
	minAPNsTokenLen = 32
	maxAPNsTokenLen = 200
)
