package auth

import (
	"context"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/auth/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthService interface {
	Refresh(ctx context.Context, refreshToken string) (newAccessToken, newRefreshToken string, err error)
}

type serverAPI struct {
	proto.UnimplementedAuthServiceServer
	service AuthService
}

func Register(server *grpc.Server, service AuthService) {
	proto.RegisterAuthServiceServer(server, &serverAPI{
		service: service,
	})
}

func (s *serverAPI) RefreshToken(
	ctx context.Context,
	request *proto.RefreshTokenRequest,
) (*proto.RefreshTokenResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if request.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	accessToken, refreshToken, err := s.service.Refresh(ctx, request.GetRefreshToken())
	if err != nil {
		return nil, err
	}

	return &proto.RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}
