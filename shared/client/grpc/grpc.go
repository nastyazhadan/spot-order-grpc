package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/models/mapper"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/spot/v6"
	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
)

type Client struct {
	api            proto.SpotInstrumentServiceClient
	circuitBreaker *gobreaker.CircuitBreaker[interface{}]
}

func New(connection *grpc.ClientConn) *Client {
	circuitBreaker := gobreaker.NewCircuitBreaker[interface{}](gobreaker.Settings{
		Name:        "spotService",
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     5 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	})

	return &Client{
		api:            proto.NewSpotInstrumentServiceClient(connection),
		circuitBreaker: circuitBreaker,
	}
}

func (client *Client) ViewMarkets(ctx context.Context, roles []models.UserRole) ([]models.Market, error) {
	result, err := client.circuitBreaker.Execute(func() (interface{}, error) {
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
	})
	if err != nil {
		return nil, fmt.Errorf("circuit breaker: %w", err)
	}

	return result.([]models.Market), nil
}
