package grpcsrv

import (
	"context"

	pb "github.com/Serajian/srosha/gen/notification/v1"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
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
