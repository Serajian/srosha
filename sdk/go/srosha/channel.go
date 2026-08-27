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
//	srosha.Email("a@b.test").From("marketing")
func (r Route) From(sender string) Route {
	r.Sender = sender
	return r
}

// One constructor per channel. They are the one place a per-channel function
// earns its place here: the body is a literal, typing "srosha." lists the
// channels, and a new channel costs a line rather than a method with logic in
// it. Compare SendEmail/SendTelegram, which would be seven identical bodies.

// Email routes to a mail address.
func Email(address string) Route { return Route{Channel: ChannelEmail, Address: address} }

// Telegram routes to a numeric chat id, or an @name.
func Telegram(address string) Route { return Route{Channel: ChannelTelegram, Address: address} }

// Bale routes to a numeric chat id, or an @name.
func Bale(address string) Route { return Route{Channel: ChannelBale, Address: address} }

// WhatsApp routes to a phone number in E.164 form, "+" and digits.
func WhatsApp(address string) Route { return Route{Channel: ChannelWhatsApp, Address: address} }

// Matrix routes to a room, and only a room: "!abc:matrix.org". The protocol has
// no way to message a person -- reaching one means finding or creating a
// private room with them, which is conversation state srosha does not keep.
func Matrix(room string) Route { return Route{Channel: ChannelMatrix, Address: room} }

// FCM routes to an Android device token.
func FCM(deviceToken string) Route { return Route{Channel: ChannelFCM, Address: deviceToken} }

// APNs routes to an Apple device token, which is hexadecimal. A token from a
// development build is unknown to production and the other way round -- see
// APNsCredential.Environment.
func APNs(deviceToken string) Route { return Route{Channel: ChannelAPNs, Address: deviceToken} }

// To is for a channel this build does not know a constructor for, which is what
// a customer reaches for when the service is newer than their SDK.
func To(channel Channel, address string) Route {
	return Route{Channel: channel, Address: address}
}
