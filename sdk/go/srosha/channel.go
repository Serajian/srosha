package srosha

// Channel is how a message travels.
//
// A string rather than the wire's integer, and that is the point: a server
// newer than this build can name a channel this build has never heard of, and
// it arrives as its own name instead of as a zero value or a panic. An SDK that
// breaks when the service grows is an SDK nobody upgrades on their own
// schedule.
type Channel string

const (
	ChannelEmail    Channel = "email"
	ChannelTelegram Channel = "telegram"
	ChannelBale     Channel = "bale"
	ChannelWhatsApp Channel = "whatsapp"
	ChannelMatrix   Channel = "matrix"
	ChannelGotify   Channel = "gotify"
	ChannelFCM      Channel = "fcm"
	ChannelAPNs     Channel = "apns"
)

func (c Channel) String() string { return string(c) }

// Route is one channel to send on, and optionally which identity to send as.
type Route struct {
	Channel Channel

	// Address is where it goes. Empty means this source's configured default
	// for the channel.
	Address string

	// Sender names one of this source's registered identities -- "marketing",
	// "alerts". Empty means whichever is default for the channel.
	Sender string
}

// From says which registered identity this route goes out as. It returns a
// copy, so a Route can be built up in one expression:
//
//	srosha.EmailTo("a@b.test").From("marketing")
func (r Route) From(sender string) Route {
	r.Sender = sender
	return r
}

// Two constructors per channel: the bare form for this source's configured
// default address, the To form for an address the message names. The bare
// form exists because that is the common case -- a source sets its address
// once in the portal and reuses it on almost every message -- and an empty
// string is a bad way to spell "use the default": a reader has to already
// know that convention to see srosha.Telegram("") as anything but a mistake.
// Writing the common case as itself, not as an omission, is worth the second
// function.
//
// Both forms keep the case a single constructor already had here: the body
// is a literal, typing "srosha." lists the channels, and a new channel costs
// two lines rather than a method with logic in it. Compare
// SendEmail/SendTelegram, which would be seven identical bodies apiece.

// Email routes to this source's default mail address.
func Email() Route { return Route{Channel: ChannelEmail} }

// EmailTo routes to a mail address.
func EmailTo(address string) Route { return Route{Channel: ChannelEmail, Address: address} }

// Telegram routes to this source's default chat.
func Telegram() Route { return Route{Channel: ChannelTelegram} }

// TelegramTo routes to a numeric chat id, or an @name.
func TelegramTo(address string) Route { return Route{Channel: ChannelTelegram, Address: address} }

// Bale routes to this source's default chat.
func Bale() Route { return Route{Channel: ChannelBale} }

// BaleTo routes to a numeric chat id, or an @name.
func BaleTo(address string) Route { return Route{Channel: ChannelBale, Address: address} }

// WhatsApp routes to this source's default number.
func WhatsApp() Route { return Route{Channel: ChannelWhatsApp} }

// WhatsAppTo routes to a phone number in E.164 form, "+" and digits.
func WhatsAppTo(address string) Route { return Route{Channel: ChannelWhatsApp, Address: address} }

// Matrix routes to this source's default room.
func Matrix() Route { return Route{Channel: ChannelMatrix} }

// MatrixTo routes to a room, and only a room: "!abc:matrix.org". The protocol
// has no way to message a person -- reaching one means finding or creating a
// private room with them, which is conversation state srosha does not keep.
func MatrixTo(room string) Route { return Route{Channel: ChannelMatrix, Address: room} }

// Gotify routes to this source's default application.
func Gotify() Route { return Route{Channel: ChannelGotify} }

// GotifyTo routes to an application id on a self-hosted Gotify server.
func GotifyTo(applicationID string) Route {
	return Route{Channel: ChannelGotify, Address: applicationID}
}

// FCM routes to this source's default device.
func FCM() Route { return Route{Channel: ChannelFCM} }

// FCMTo routes to an Android device token.
func FCMTo(deviceToken string) Route { return Route{Channel: ChannelFCM, Address: deviceToken} }

// APNs routes to this source's default device.
func APNs() Route { return Route{Channel: ChannelAPNs} }

// APNsTo routes to an Apple device token, which is hexadecimal. A token from a
// development build is unknown to production and the other way round -- see
// APNsCredential.Environment.
func APNsTo(deviceToken string) Route { return Route{Channel: ChannelAPNs, Address: deviceToken} }

// To is for a channel this build does not know a constructor for, which is what
// a customer reaches for when the service is newer than their SDK.
func To(channel Channel, address string) Route {
	return Route{Channel: channel, Address: address}
}
