package shared

const (
	idLength      = 26
	crockfordBase = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // no I, L, O, U

	// Telegram's own limits for an @name, which Bale follows.
	minUsernameLen = 5
	maxUsernameLen = 32

	// E.164 allows 8 to 15 digits after the "+".
	minE164Digits = 8
	maxE164Digits = 15
)
