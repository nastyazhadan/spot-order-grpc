package grpc

import (
	"context"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/models/mapper"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"

	"google.golang.org/grpc"
)

type Client struct {
	api proto.SpotInstrumentServiceClient
}

func New(connection *grpc.ClientConn) *Client {
	return &Client{
		api: proto.NewSpotInstrumentServiceClient(connection),
	}
}

func (client *Client) ViewMarkets(ctx context.Context, roles []models.UserRole) ([]models.Market, error) {

	userRoles := make([]proto.UserRole, 0, len(roles))
	for _, role := range roles {
		userRoles = append(userRoles, mapper.UserRoleToProto(role))
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
