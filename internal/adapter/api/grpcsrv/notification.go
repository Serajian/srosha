package grpcsrv

import (
	"context"

	pb "github.com/Serajian/srosha/gen/notification/v1"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

// NotificationServer is the gRPC face of the submit and query use cases. It
// holds no logic of its own: read who is calling, translate, call, translate
// back. Anything that looks like a decision here belongs in the core.
type NotificationServer struct {
	pb.UnimplementedNotificationServiceServer

	submitter *usecase.Submitter
	querier   *usecase.Querier
}

func NewNotificationServer(
	submitter *usecase.Submitter, querier *usecase.Querier,
) (*NotificationServer, error) {
	if submitter == nil || querier == nil {
		return nil, errs.InternalErr("notification server is missing a use case")
	}
	return &NotificationServer{submitter: submitter, querier: querier}, nil
}

func (s *NotificationServer) Submit(
	ctx context.Context, req *pb.SubmitRequest,
) (*pb.SubmitResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	cmd, err := toSubmitCommand(src.ID, req)
	if err != nil {
		return nil, err
	}

	result, err := s.submitter.Submit(ctx, cmd)
	if err != nil {
		return nil, err
	}

	return &pb.SubmitResponse{
		Id:                result.ID.String(),
		EffectivePriority: fromPriority(result.EffectivePriority),
		Downgraded:        result.Downgraded,
		Duplicate:         result.Duplicate,
	}, nil
}

// Get passes the caller's own id to the use case, which reports a message
// belonging to somebody else as not found. Not as forbidden: telling a caller
// that an id exists but is not theirs is how they find out which ids exist.
func (s *NotificationServer) Get(
	ctx context.Context, req *pb.NotificationServiceGetRequest,
) (*pb.NotificationServiceGetResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	id, err := shared.ParseID(req.GetId())
	if err != nil {
		return nil, err
	}

	cursor, err := toCursor(req.GetPage())
	if err != nil {
		return nil, err
	}

	result, err := s.querier.Get(ctx, src.ID, id, cursor)
	if err != nil {
		return nil, err
	}

	return &pb.NotificationServiceGetResponse{
		Notification:  fromNotification(result.Notification),
		Deliveries:    fromDeliveries(result.Deliveries),
		NextPageToken: nextPageToken(result.Deliveries),
	}, nil
}

// errUnidentified is what a handler answers when the auth interceptor is not in
// front of it. It should be unreachable, and it is an internal error rather
// than an unauthenticated one because the caller did nothing wrong -- we wired
// the server incorrectly.
func errUnidentified() error {
	return errs.InternalErr("the request could not be completed").
		WithStr("handler reached with no authenticated source")
}
