package grpcsrv

import (
	"context"
	"time"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"

	"google.golang.org/protobuf/types/known/durationpb"
)

// Limits are the operational numbers a caller is subject to.
//
// They are configuration, and this package reads none -- bootstrap passes them
// in, as it does everything else here. Two numbers rather than the whole config
// struct, because these are the only two a caller is entitled to know.
type Limits struct {
	// Retention is how long a message and its deliveries are kept.
	Retention time.Duration

	// RateLimitPerMinute is counted per source.
	RateLimitPerMinute int
}

// SourceServer answers a caller's questions about itself.
type SourceServer struct {
	pb.UnimplementedSourceServiceServer

	limits Limits
}

func NewSourceServer(limits Limits) *SourceServer {
	return &SourceServer{limits: limits}
}

// Whoami reads the source the auth interceptor already resolved, and adds the
// two limits it is subject to.
//
// It touches no repository. Authenticating the call already fetched the row, so
// asking again would be a second query for something in hand -- and it means
// this answer cannot be stale in a way the request itself was not.
func (s *SourceServer) Whoami(
	ctx context.Context, _ *pb.WhoamiRequest,
) (*pb.WhoamiResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	addresses := make(map[string]string, len(src.DefaultAddresses))
	for channel, address := range src.DefaultAddresses {
		addresses[channel.String()] = address
	}

	return &pb.WhoamiResponse{
		Id:                 src.ID,
		Name:               src.Name,
		MaxPriority:        fromPriority(src.MaxPriority),
		AllowCustomAddress: src.AllowCustomAddress,
		DefaultAddresses:   addresses,
		Retention:          durationpb.New(s.limits.Retention),
		//nolint:gosec // a per-minute request count, from config and bounded above zero
		RateLimitPerMinute: int32(s.limits.RateLimitPerMinute),
	}, nil
}
