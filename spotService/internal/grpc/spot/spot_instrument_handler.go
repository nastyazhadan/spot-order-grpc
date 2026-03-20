package spot

import (
	"context"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	mapper "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/inbound"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SpotInstrument interface {
	ViewMarkets(ctx context.Context, userRoles []models.UserRole) ([]models.Market, error)
	GetMarketByID(ctx context.Context, id uuid.UUID) (models.Market, error)
}

type serverAPI struct {
	proto.UnimplementedSpotInstrumentServiceServer
	spotInstrument SpotInstrument
}

func Register(server *grpc.Server, spotInstrument SpotInstrument) {
	proto.RegisterSpotInstrumentServiceServer(
		server, &serverAPI{
			spotInstrument: spotInstrument,
		})
}

func (s *serverAPI) ViewMarkets(
	ctx context.Context,
	request *proto.ViewMarketsRequest,
) (*proto.ViewMarketsResponse, error) {
	userRoles, err := s.validateUserRoles(request)
	if err != nil {
		return nil, err
	}

	markets, err := s.spotInstrument.ViewMarkets(ctx, userRoles)
	if err != nil {
		return nil, err
	}

	out := make([]*proto.Market, 0, len(markets))
	for _, market := range markets {
		out = append(out, mapper.MarketToProto(market))
	}

	return &proto.ViewMarketsResponse{
		Markets: out,
	}, nil
}

func (s *serverAPI) GetMarketByID(
	ctx context.Context,
	request *proto.GetMarketByIDRequest,
) (*proto.GetMarketByIDResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, errors.MsgRequestRequired)
	}

	marketID, err := uuid.Parse(request.GetMarketId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid market_id")
	}

	market, err := s.spotInstrument.GetMarketByID(ctx, marketID)
	if err != nil {
		return nil, err
	}

	return &proto.GetMarketByIDResponse{
		Market: mapper.MarketToProto(market),
	}, nil
}

func (s *serverAPI) validateUserRoles(
	request *proto.ViewMarketsRequest,
) ([]models.UserRole, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, errors.MsgRequestRequired)
	}

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
