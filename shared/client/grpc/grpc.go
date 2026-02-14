package grpc

import (
	"context"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/models/mapper"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"

	"google.golang.org/grpc"
)

const timeout = time.Second * 3

type Client struct {
	api proto.SpotInstrumentServiceClient
}

func New(connection *grpc.ClientConn) *Client {
	return &Client{
		api: proto.NewSpotInstrumentServiceClient(connection),
	}
}

func (client *Client) ViewMarkets(ctx context.Context, roles []int32) ([]models.Market, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	userRoles := make([]proto.UserRole, 0, len(roles))
	for _, role := range roles {
		userRoles = append(userRoles, proto.UserRole(role))
	}

	if len(userRoles) == 0 {
		userRoles = []proto.UserRole{proto.UserRole_ROLE_VIEWER}
	}

	response, err := client.api.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: userRoles,
	})
	if err != nil {
		return nil, err
	}

	out := make([]models.Market, 0, len(response.GetMarkets()))
	for _, market := range response.GetMarkets() {
		mappedMarket, err := mapper.MarketFromProto(market)
		if err != nil {
			return nil, err
		}
		out = append(out, mappedMarket)
	}

	return out, nil
}
