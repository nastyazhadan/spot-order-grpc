package grpc

import (
	"context"
	"fmt"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/breaker"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/mapper"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type SpotClient struct {
	api                proto.SpotInstrumentServiceClient
	viewMarketsBreaker *gobreaker.CircuitBreaker[*proto.ViewMarketsResponse]
}

func NewSpotClient(connection *grpc.ClientConn, cfg config.CircuitBreakerConfig) *SpotClient {
	return &SpotClient{
		api:                proto.NewSpotInstrumentServiceClient(connection),
		viewMarketsBreaker: breaker.New[*proto.ViewMarketsResponse]("spotService.ViewMarkets", cfg),
	}
}

func (c *SpotClient) ViewMarkets(ctx context.Context, roles []models.UserRole) ([]models.Market, error) {
	userRoles := make([]proto.UserRole, 0, len(roles))
	for _, role := range roles {
		userRoles = append(userRoles, mapper.UserRoleToProto(role))
	}

	response, err := c.viewMarketsBreaker.Execute(func() (*proto.ViewMarketsResponse, error) {
		resp, err := c.api.ViewMarkets(ctx, &proto.ViewMarketsRequest{
			UserRoles: userRoles,
		})
		if err != nil {
			zapLogger.Error(ctx, "spotService.ViewMarkets failed", zap.Error(err))
			return nil, err
		}
		return resp, nil
	})
	if err != nil {
		return nil, fmt.Errorf("view markets via circuit breaker: %w", err)
	}

	out := make([]models.Market, 0, len(response.GetMarkets()))
	for _, market := range response.GetMarkets() {
		mappedMarket, err := mapper.MarketFromProto(market)
		if err != nil {
			return nil, fmt.Errorf("map market from proto: %w", err)
		}
		out = append(out, mappedMarket)
	}

	return out, nil
}
