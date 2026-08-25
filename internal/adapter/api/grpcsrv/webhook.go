package grpcsrv

import (
	"context"

	pb "github.com/Serajian/srosha/gen/notification/v1"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

// WebhookServer is the gRPC face of the registrar. One source has one callback,
// and which source is calling comes from the context rather than the request,
// so there is nothing here for a caller to point at somebody else's.
type WebhookServer struct {
	pb.UnimplementedWebhookServiceServer

	registrar *usecase.Registrar
}

func NewWebhookServer(registrar *usecase.Registrar) (*WebhookServer, error) {
	if registrar == nil {
		return nil, errs.InternalErr("webhook server is missing its use case")
	}
	return &WebhookServer{registrar: registrar}, nil
}

func (s *WebhookServer) Register(
	ctx context.Context, req *pb.RegisterRequest,
) (*pb.RegisterResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	w, err := s.registrar.Register(ctx, src.ID, webhook.Registration{
		CallbackURL: req.GetCallbackUrl(),
	})
	if err != nil {
		return nil, err
	}
	return &pb.RegisterResponse{Webhook: fromWebhook(w)}, nil
}

func (s *WebhookServer) Get(
	ctx context.Context, _ *pb.WebhookServiceGetRequest,
) (*pb.WebhookServiceGetResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	w, err := s.registrar.Get(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	return &pb.WebhookServiceGetResponse{Webhook: fromWebhook(w)}, nil
}

// Deactivate and Activate answer with the callback as it now stands rather than
// with nothing. A caller that has just changed something wants to see what it
// became, and reading it back would be a second round trip to learn what this
// one already knows.
func (s *WebhookServer) Deactivate(
	ctx context.Context, _ *pb.DeactivateRequest,
) (*pb.DeactivateResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	if err := s.registrar.Deactivate(ctx, src.ID); err != nil {
		return nil, err
	}

	w, err := s.registrar.Get(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	return &pb.DeactivateResponse{Webhook: fromWebhook(w)}, nil
}

func (s *WebhookServer) Activate(
	ctx context.Context, _ *pb.ActivateRequest,
) (*pb.ActivateResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	if err := s.registrar.Activate(ctx, src.ID); err != nil {
		return nil, err
	}

	w, err := s.registrar.Get(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	return &pb.ActivateResponse{Webhook: fromWebhook(w)}, nil
}
