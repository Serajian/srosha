package srosha

import (
	"context"
	"time"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
)

// Identity of the caller: who srosha thinks you are, and what you are allowed.
type Me struct {
	// ID is your source id. It is what to quote when asking for help.
	ID   string
	Name string

	// MaxPriority is the highest you may send at. Asking above it is not an
	// error -- the message is accepted at this and Receipt.Downgraded says so.
	MaxPriority Priority

	// AllowCustomAddress says whether a message may name its own recipient.
	// When false, DefaultAddresses is the whole of where you can reach.
	AllowCustomAddress bool

	// DefaultAddresses is what a route with no address resolves to, per
	// channel. A channel missing from it has no default.
	DefaultAddresses map[Channel]string

	// Retention is how long a message and its deliveries are kept. A listing
	// window may not reach further back than this.
	Retention time.Duration

	// RateLimitPerMinute is counted per source. Going over is ErrRateLimited.
	RateLimitPerMinute int
}

// MaxWindow is the longest listing window this deployment will serve.
//
// Derived from Retention rather than sent, because the honest number is the
// duration: a deployment keeping ten days has no Window that says so, and this
// rounds down to the one it will accept.
func (m Me) MaxWindow() Window {
	switch {
	case m.Retention >= 30*24*time.Hour:
		return LastMonth
	case m.Retention >= 7*24*time.Hour:
		return LastWeek
	case m.Retention >= 24*time.Hour:
		return LastDay
	case m.Retention >= time.Hour:
		return LastHour
	default:
		// Shorter than the shortest window there is. Everything still works,
		// because it names no number of its own.
		return Everything
	}
}

// Whoami says who srosha thinks you are and what you are allowed.
//
// It answers two questions that nothing else answers until you get them wrong:
// your priority ceiling, which otherwise shows up as a message that was
// quietly lowered, and the retention window, which otherwise shows up as a
// listing that was refused.
//
// It is also the cheapest way to find out that the address is right and the key
// works. A client connects lazily, so without this the first news of either
// being wrong arrives on the first message that mattered:
//
//	c, err := srosha.New(ctx, addr, key)
//	if err != nil {
//	    return err
//	}
//	if me, err := c.Whoami(ctx); err != nil {
//	    log.Warn("srosha unreachable at startup", "err", err)
//	} else {
//	    log.Info("srosha", "as", me.Name, "ceiling", me.MaxPriority)
//	}
//
// Call it when a process starts, not on a timer. It is not a health check: it
// counts against your rate limit like every other call, and an answer says
// nothing about the next one -- keys are revoked and networks part in between.
//
// Failing it is not a reason to refuse to start. srosha is asynchronous, and an
// application that will not boot while it is briefly down is worse than one
// that logs the warning and carries on.
func (c *Client) Whoami(ctx context.Context) (Me, error) {
	var res *pb.WhoamiResponse
	if err := c.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.sources.Whoami(ctx, &pb.WhoamiRequest{})
		return err
	}); err != nil {
		return Me{}, err
	}

	addresses := make(map[Channel]string, len(res.GetDefaultAddresses()))
	for channel, address := range res.GetDefaultAddresses() {
		addresses[Channel(channel)] = address
	}

	return Me{
		ID:                 res.GetId(),
		Name:               res.GetName(),
		MaxPriority:        fromPriority(res.GetMaxPriority()),
		AllowCustomAddress: res.GetAllowCustomAddress(),
		DefaultAddresses:   addresses,
		Retention:          res.GetRetention().AsDuration(),
		RateLimitPerMinute: int(res.GetRateLimitPerMinute()),
	}, nil
}
