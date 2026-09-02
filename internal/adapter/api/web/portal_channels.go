package web

import "github.com/Serajian/srosha/internal/core/shared"

// channelGuide is what the senders form says about one channel: what its
// Secret is, and what belongs in Settings.
//
// The form used to say neither. It offered one placeholder for all eight --
// {"host":"smtp.acme.test"} -- which is wrong for five of them and actively
// misleading for the three that read no settings at all, and it pointed at the
// SDK's README, which documents Go field names rather than the json keys this
// form parses.
//
// It is written here rather than read from internal/adapter/sender/<channel>,
// because one adapter never imports another. So this table and those
// ParseConfig functions are two statements of the same thing and can drift.
// What holds the SET together is TestEveryChannelIsOnTheSendersForm, which
// fails the day a ninth channel is added with no line here. The json keys
// inside each line are held by nothing but review -- see the change report.
type channelGuide struct {
	Channel shared.Channel

	// Identity is what the Secret field wants on this channel, and it is the
	// thing that differs most between them: a bot token, a whole json file,
	// the contents of a .p8.
	//
	// Not named for the field it describes: gosec reads a struct field called
	// Secret with a string literal beside it as a credential somebody hardcoded,
	// which is exactly the right thing for it to do and exactly wrong here.
	Identity string

	// Settings is an example of the json this channel parses, and it becomes
	// the placeholder once the channel is picked. Empty for the three that
	// read none.
	Settings string

	// Hint is the sentence under the Settings field, naming the keys.
	Hint string
}

// channelGuides is every channel, in the order shared.AllChannels lists them,
// so the form's <select> and the guidance under it are one list and not two.
var channelGuides = []channelGuide{
	{
		Channel:  shared.ChannelEmail,
		Identity: "the mailbox password.",
		Settings: `{"host":"smtp.acme.test","port":587,"username":"noreply@acme.test","from":"noreply@acme.test"}`,
		Hint: "host, port, username, from, content_type. Port 0 means 587, " +
			"465 is TLS from the first byte, anything else is STARTTLS. " +
			"content_type is text/plain or text/html, and empty means plain.",
	},
	{
		Channel:  shared.ChannelTelegram,
		Identity: "the bot token BotFather gave you, whole.",
		Hint:     "This channel takes no settings — the token is the whole identity.",
	},
	{
		Channel:  shared.ChannelBale,
		Identity: "the bot token from Bale's BotFather, whole.",
		Hint:     "This channel takes no settings — the token is the whole identity.",
	},
	{
		Channel:  shared.ChannelWhatsApp,
		Identity: "the access token of your Meta app.",
		Settings: `{"phone_number_id":"123456789012345"}`,
		Hint: "phone_number_id — the id Meta issues for the number you send " +
			"from, not the number itself.",
	},
	{
		Channel:  shared.ChannelMatrix,
		Identity: "the access token of the account that posts.",
		Settings: `{"homeserver":"https://matrix.acme.test"}`,
		Hint: "homeserver — https, and a bare address: no path, no query, " +
			"no credentials in it.",
	},
	{
		Channel:  shared.ChannelGotify,
		Identity: "the application token. It alone decides which application a message lands in.",
		Settings: `{"server_url":"https://gotify.acme.test"}`,
		Hint: "server_url — your own server, https, and a bare address: " +
			"no path, no query, no credentials in it.",
	},
	{
		Channel:  shared.ChannelFCM,
		Identity: "the whole service account json file, pasted in. The project is inside it.",
		Hint:     "This channel takes no settings — the service account is the whole identity.",
	},
	{
		Channel:  shared.ChannelAPNs,
		Identity: "the contents of the .p8 signing key, pasted in, BEGIN and END lines with it.",
		Settings: `{"key_id":"ABC1234567","team_id":"DEF7654321","topic":"ir.acme.app","environment":"production"}`,
		Hint: "key_id, team_id, topic, environment. topic is the app's bundle " +
			"id; environment is production or sandbox, and empty means " +
			"production. None of the four is a secret — only the key is.",
	},
}
