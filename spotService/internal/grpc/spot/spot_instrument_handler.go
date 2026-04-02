package spot

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	mapper "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/inbound"
)

type SpotInstrument interface {
	ViewMarkets(ctx context.Context, limit, offset uint64) ([]models.Market, uint64, bool, error)
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
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, errors.MsgRequestRequired)
	}

	markets, nextOffset, hasMore, err := s.spotInstrument.ViewMarkets(ctx, request.GetLimit(), request.GetOffset())
	if err != nil {
		return nil, err
	}

	out := make([]*proto.Market, 0, len(markets))
	for _, market := range markets {
		out = append(out, mapper.MarketToProto(market))
	}

	return &proto.ViewMarketsResponse{
		Markets:    out,
		NextOffset: nextOffset,
		HasMore:    hasMore,
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
