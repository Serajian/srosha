package registry

import (
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/infra/appleauth"
)

// AppleTokens opens the signing of APNs provider tokens, and registers nothing
// to close.
//
// Nothing to close and nothing to dial: Apple has no token endpoint, so a
// provider token is a JWT signed here and simply presented. What is held is the
// last token each identity was given.
//
// That holding is the whole reason this is opened rather than called. Apple
// wants a provider token refreshed at least hourly and refuses one signed more
// often than every twenty minutes -- and SenderRegistry.For builds a sender per
// message, so without somewhere to keep the last one, a busy app would be
// answered with TooManyProviderTokenUpdates.
func AppleTokens(now shared.NowFunc) (*appleauth.Minter, error) {
	return appleauth.NewMinter(now)
}
