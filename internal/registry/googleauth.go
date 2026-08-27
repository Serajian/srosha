package registry

import (
	"net/http"

	"github.com/Serajian/srosha/internal/infra/googleauth"
)

// GoogleTokens opens the exchange that turns a service account into an access
// token, and registers nothing to close.
//
// Nothing to close because nothing is held open: the exchange is an http call
// through the client below, and what is kept between calls is the last token
// each account was given -- memory, not a connection.
//
// A minter rather than a token, because a token lasts an hour and every source
// may bring its own service account. It caches per account, which is the whole
// reason it exists: a sender is built per message, and minting a token each
// time would put an RSA signature and a round trip in front of every push.
func GoogleTokens(client *http.Client) (*googleauth.Minter, error) {
	return googleauth.NewMinter(client, googleauth.ScopeFCM)
}
