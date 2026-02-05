package spot_instrument

import (
	"context"
	"spotOrder/internal/domain/models"
	"spotOrder/internal/mapper"

	spot_orderv1 "github.com/nastyazhadan/protos/gen/go/spot_order"
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
	spot_orderv1.UnimplementedSpotInstrumentServiceServer
	spotInstrument SpotInstrument
}

func Register(gRPC *grpc.Server, spotInstrument SpotInstrument) {
	spot_orderv1.RegisterSpotInstrumentServiceServer(
		gRPC, &serverAPI{
			spotInstrument: spotInstrument,
		})
}

func (s *serverAPI) ViewMarkets(
	ctx context.Context,
	req *spot_orderv1.ViewMarketsRequest,
) (*spot_orderv1.ViewMarketsResponse, error) {

	// roles пока просто принимаем — но валидировать можно минимально:
	roles := make([]int32, 0, len(req.GetUserRoles()))
	for _, role := range req.GetUserRoles() {
		roles = append(roles, int32(role))
	}

	markets, err := s.spotInstrument.ViewMarkets(ctx, roles)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	out := make([]*spot_orderv1.Market, 0, len(markets))
	for _, market := range markets {
		out = append(out, mapper.MapToProto(market))
	}

	return &spot_orderv1.ViewMarketsResponse{
		Markets: out,
	}, nil
}
