package settings

import (
	"net/url"
	"strings"

	"github.com/Serajian/srosha/pkg/env"
)

// checkURLPassword refuses to start on a credential too short to be one.
//
// It exists because on 2026-09-01 two of the three NATS passwords turned out to
// be eight characters, in production, for however long. The rule was already
// written -- `openssl rand -hex 24`, in docs/reference -- and nothing enforced
// it. No test failed, no check complained, no log line said a word: a short
// password is exactly as quiet as a long one. What finally caught them was an
// unrelated tool refusing to hash a password under ten characters.
//
// So this is that rule, moved from a document into the only place that can
// stop it: the binary does not come up.
//
// **Production only.** A laptop's postgres is `srosha` with the password
// `srosha`, and it should stay that way -- a check that makes local development
// unpleasant is a check somebody switches off. Same reasoning as
// NOTIF_CONSOLE_SECURE_COOKIE.
//
// It looks at the password inside the url and nothing else. Provider passwords
// -- SMTP, a bot token -- are not ours to set: refusing one because a mail host
// issued something short would be this service telling a customer their own
// provider is wrong.
func checkURLPassword(r *env.Reader, production bool, key string, raw env.Secret) {
	if !production {
		return
	}

	u, err := url.Parse(raw.Reveal())
	if err != nil {
		// Not this function's business. Whatever opens the connection reports a
		// malformed url far better than a length check could.
		return
	}
	if u.User == nil {
		return
	}
	pw, ok := u.User.Password()
	if !ok {
		return
	}

	// The length, never the value: this message is written to a log.
	r.Check(len(pw) >= minCredentialLen,
		"NOTIF_%s carries a %d-character password, and production wants at least "+
			"%d. Generate one with `openssl rand -hex 24`: hex only, so nothing "+
			"in it can be eaten by a shell, a compose file or a url",
		key, len(pw), minCredentialLen)
}

// checkNkeySeed refuses a seed that is not one, at startup rather than at the
// first connection.
//
// A user seed is `S` for seed, `U` for user, and 56 more characters of base32.
// Checking the shape here means a typo is a message naming the key, instead of
// an authorization failure the broker reports and this service reads as "the
// broker refused us".
func checkNkeySeed(r *env.Reader, seed env.Secret) {
	v := seed.Reveal()

	// The prefix and the length, never the value.
	r.Check(len(v) == nkeySeedLen,
		"NOTIF_MQ_NKEY_SEED is %d characters, and a user seed is %d",
		len(v), nkeySeedLen)
	r.Check(strings.HasPrefix(v, nkeyUserSeedPrefix),
		"NOTIF_MQ_NKEY_SEED does not begin with %q, so it is not a user seed. "+
			"Generate one with `nk -gen user -pubout`: the line starting %q is "+
			"this, and the one starting \"U\" goes in nats-server.conf",
		nkeyUserSeedPrefix, nkeyUserSeedPrefix)
}
