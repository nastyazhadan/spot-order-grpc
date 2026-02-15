package grpc

import (
	"context"
	"errors"
	"sort"

	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/models/mapper"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SpotInstrument interface {
	ViewMarkets(
		ctx context.Context,
		userRoles []uint8,
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

func (server *serverAPI) ViewMarkets(
	ctx context.Context,
	request *proto.ViewMarketsRequest,
) (*proto.ViewMarketsResponse, error) {

	roles := make([]uint8, 0, len(request.GetUserRoles()))
	for _, role := range request.GetUserRoles() {
		if role == proto.UserRole_ROLE_UNSPECIFIED {
			return nil, status.Error(codes.InvalidArgument, "user role not specified")
		}

		roles = append(roles, mapper.UserRoleFromProto(role))
	}

	markets, err := server.spotInstrument.ViewMarkets(ctx, roles)
	if err != nil {
		if errors.Is(err, serviceErrors.ErrMarketsNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
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
