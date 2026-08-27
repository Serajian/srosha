package grpcsrv

import (
	"context"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
)

// CredentialServer is the gRPC face of the sending identities.
//
// Which source an identity belongs to comes from the context, never from the
// request: read from a body, a caller could register a bot under somebody
// else's name and then send as them.
type CredentialServer struct {
	pb.UnimplementedCredentialServiceServer

	credentials *usecase.Credentials
}

func NewCredentialServer(credentials *usecase.Credentials) (*CredentialServer, error) {
	if credentials == nil {
		return nil, errs.InternalErr("credential server is missing its use case")
	}
	return &CredentialServer{credentials: credentials}, nil
}

func (s *CredentialServer) Register(
	ctx context.Context, req *pb.CredentialServiceRegisterRequest,
) (*pb.CredentialServiceRegisterResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	reg, err := toCredentialRegistration(req)
	if err != nil {
		return nil, err
	}

	c, err := s.credentials.Register(ctx, src.ID, reg)
	if err != nil {
		return nil, err
	}
	return &pb.CredentialServiceRegisterResponse{Credential: fromCredential(c)}, nil
}

func (s *CredentialServer) List(
	ctx context.Context, req *pb.CredentialServiceListRequest,
) (*pb.CredentialServiceListResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	got, err := s.credentials.List(ctx, src.ID)
	if err != nil {
		return nil, err
	}

	// Filtered here rather than in a second query: a source has a handful of
	// identities, and the alternative is a statement that exists only to save
	// a loop over four rows.
	if req.GetChannel() != pb.Channel_CHANNEL_UNSPECIFIED {
		channel, err := toChannel(req.GetChannel())
		if err != nil {
			return nil, err
		}
		got = onChannel(got, channel)
	}

	return &pb.CredentialServiceListResponse{Credentials: fromCredentials(got)}, nil
}

func (s *CredentialServer) Deactivate(
	ctx context.Context, req *pb.CredentialServiceDeactivateRequest,
) (*pb.CredentialServiceDeactivateResponse, error) {
	c, err := s.act(ctx, req.GetId(), s.credentials.Deactivate)
	if err != nil {
		return nil, err
	}
	return &pb.CredentialServiceDeactivateResponse{Credential: fromCredential(c)}, nil
}

func (s *CredentialServer) Activate(
	ctx context.Context, req *pb.CredentialServiceActivateRequest,
) (*pb.CredentialServiceActivateResponse, error) {
	c, err := s.act(ctx, req.GetId(), s.credentials.Activate)
	if err != nil {
		return nil, err
	}
	return &pb.CredentialServiceActivateResponse{Credential: fromCredential(c)}, nil
}

func (s *CredentialServer) SetDefault(
	ctx context.Context, req *pb.CredentialServiceSetDefaultRequest,
) (*pb.CredentialServiceSetDefaultResponse, error) {
	c, err := s.act(ctx, req.GetId(), s.credentials.SetDefault)
	if err != nil {
		return nil, err
	}
	return &pb.CredentialServiceSetDefaultResponse{Credential: fromCredential(c)}, nil
}

func (s *CredentialServer) Rotate(
	ctx context.Context, req *pb.CredentialServiceRotateRequest,
) (*pb.CredentialServiceRotateResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	c, err := s.credentials.Rotate(ctx, src.ID, shared.ID(req.GetId()), req.GetSecret())
	if err != nil {
		return nil, err
	}
	return &pb.CredentialServiceRotateResponse{Credential: fromCredential(c)}, nil
}

func (s *CredentialServer) Update(
	ctx context.Context, req *pb.CredentialServiceUpdateRequest,
) (*pb.CredentialServiceUpdateResponse, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}

	var config []byte
	if raw := req.GetConfig(); raw != "" {
		config = []byte(raw)
	}

	c, err := s.credentials.Update(ctx, src.ID, shared.ID(req.GetId()), config)
	if err != nil {
		return nil, err
	}
	return &pb.CredentialServiceUpdateResponse{Credential: fromCredential(c)}, nil
}

// act is the shape the three flag rpcs share: identify the caller, hand the id
// to the use case scoped by them, answer with the identity as it now stands.
func (s *CredentialServer) act(
	ctx context.Context,
	id string,
	do func(context.Context, string, shared.ID) (*credential.Credential, error),
) (*credential.Credential, error) {
	src, ok := SourceFrom(ctx)
	if !ok {
		return nil, errUnidentified()
	}
	return do(ctx, src.ID, shared.ID(id))
}
