package spot

import (
	"context"
	"errors"
	"sort"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/models/mapper"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SpotInstrument interface {
	ViewMarkets(
		ctx context.Context,
		userRoles []models.UserRole,
	) ([]models.Market, error)
}

type serverAPI struct {
	proto.UnimplementedSpotInstrumentServiceServer
	spotInstrument SpotInstrument
}

func Register(gRPC *grpc.Server, spotInstrument SpotInstrument) {
	proto.RegisterSpotInstrumentServiceServer(
		gRPC, &serverAPI{
			spotInstrument: spotInstrument,
		})
}

func (s *serverAPI) ViewMarkets(
	ctx context.Context,
	request *proto.ViewMarketsRequest,
) (*proto.ViewMarketsResponse, error) {

	userRoles, err := s.getUserRoles(request)
	if err != nil {
		return nil, err
	}

	markets, err := s.spotInstrument.ViewMarkets(ctx, userRoles)
	if err != nil {
		if errors.Is(err, serviceErrors.ErrMarketsNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}

		zapLogger.Error(ctx, "failed to view markets",
			zap.Error(err))
		return nil, status.Error(codes.Internal, "internal error")
	}

	out := make([]*proto.Market, 0, len(markets))
	for _, market := range markets {
		out = append(out, mapper.MarketToProto(market))
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return &proto.ViewMarketsResponse{
		Markets: out,
	}, nil
}

func (s *serverAPI) getUserRoles(request *proto.ViewMarketsRequest) ([]models.UserRole, error) {
	roles := request.GetUserRoles()

	if len(roles) == 0 {
		return nil, status.Error(codes.InvalidArgument, "user roles are required")
	}

	seen := make(map[proto.UserRole]struct{}, len(roles))
	userRoles := make([]models.UserRole, 0, len(roles))

	for _, role := range roles {
		if role == proto.UserRole_ROLE_UNSPECIFIED {
			return nil, status.Error(codes.InvalidArgument, "user role not specified")
		}

		if _, found := seen[role]; found {
			return nil, status.Error(codes.InvalidArgument, "duplicate user role found")
		}
		seen[role] = struct{}{}

		userRoles = append(userRoles, mapper.UserRoleFromProto(role))
	}

	return userRoles, nil
}
