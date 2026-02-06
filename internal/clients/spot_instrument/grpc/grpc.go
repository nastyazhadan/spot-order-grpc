package spot_instrument

import (
	"context"
	"time"

	spot_orderv1 "github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/grpc"
)

type Client struct {
	api spot_orderv1.SpotInstrumentServiceClient
}

func New(conn *grpc.ClientConn) *Client {
	return &Client{
		api: spot_orderv1.NewSpotInstrumentServiceClient(conn),
	}
}

func (c *Client) ViewMarkets(ctx context.Context, roles []spot_orderv1.UserRole) (*spot_orderv1.ViewMarketsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return c.api.ViewMarkets(ctx, &spot_orderv1.ViewMarketsRequest{
		UserRoles: roles,
	})
}
