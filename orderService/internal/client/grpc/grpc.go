package grpc

import (
	"context"
	"spotOrder/internal/domain/models"
	"spotOrder/internal/mapper"
	"time"

	proto "github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/grpc"
)

const timeout = time.Second * 3

type Client struct {
	api proto.SpotInstrumentServiceClient
}

func New(conn *grpc.ClientConn) *Client {
	return &Client{
		api: proto.NewSpotInstrumentServiceClient(conn),
	}
}

func (c *Client) ViewMarkets(ctx context.Context, roles []int32) ([]models.Market, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	userRoles := make([]proto.UserRole, 0, len(roles))
	for _, role := range roles {
		userRoles = append(userRoles, proto.UserRole(role))
	}

	resp, err := c.api.ViewMarkets(ctx, &proto.ViewMarketsRequest{
		UserRoles: userRoles,
	})
	if err != nil {
		return nil, err
	}

	out := make([]models.Market, 0, len(resp.GetMarkets()))
	for _, m := range resp.GetMarkets() {
		out = append(out, mapper.MarketFromProto(m))
	}

	return out, nil
}
