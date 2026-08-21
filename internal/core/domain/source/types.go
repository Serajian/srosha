package source

import "github.com/Serajian/srosha/internal/core/shared"

// Route is one channel a client asked for. Address is optional: empty means
// this source's configured default for that channel.
type Route struct {
	Channel shared.Channel
	Address string
}
