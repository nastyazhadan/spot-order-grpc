package grpc

import (
	"context"

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
		userRoles []int32,
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

	roles := make([]int32, 0, len(request.GetUserRoles()))
	for _, role := range request.GetUserRoles() {
		roles = append(roles, int32(role))
	}

	markets, err := server.spotInstrument.ViewMarkets(ctx, roles)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	out := make([]*proto.Market, 0, len(markets))
	for _, market := range markets {
		out = append(out, mapper.MarketToProto(market))
	}

	return &proto.ViewMarketsResponse{
		Markets: out,
	}, nil
}
