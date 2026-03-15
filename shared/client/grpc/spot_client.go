package grpc

import (
	"context"
	"fmt"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/mapper"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type SpotClient struct {
	api            proto.SpotInstrumentServiceClient
	circuitBreaker *gobreaker.CircuitBreaker[[]models.Market]
}

func NewSpotClient(connection *grpc.ClientConn, cfg config.CircuitBreakerConfig) *SpotClient {
	circuitBreaker := gobreaker.NewCircuitBreaker[[]models.Market](gobreaker.Settings{
		Name:        "spotService",
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			shouldTrip := counts.ConsecutiveFailures >= cfg.MaxFailures

			if shouldTrip {
				zapLogger.Warn(context.Background(), "circuit breaker is about to open",
					zap.String("name", "spotService"),
					zap.Uint32("consecutive_failures", counts.ConsecutiveFailures),
					zap.Uint32("total_failures", counts.TotalFailures),
					zap.Uint32("total_requests", counts.Requests),
				)
			}

			return shouldTrip
		},
	})

	return &SpotClient{
		api:            proto.NewSpotInstrumentServiceClient(connection),
		circuitBreaker: circuitBreaker,
	}
}

func (c *SpotClient) ViewMarkets(ctx context.Context, roles []models.UserRole) ([]models.Market, error) {

	markets, err := c.circuitBreaker.Execute(func() ([]models.Market, error) {
		userRoles := make([]proto.UserRole, 0, len(roles))
		for _, role := range roles {
			userRoles = append(userRoles, mapper.UserRoleToProto(role))
		}

		response, err := c.api.ViewMarkets(ctx, &proto.ViewMarketsRequest{
			UserRoles: userRoles,
		})
		if err != nil {
			zapLogger.Error(ctx, "ViewMarkets failed", zap.Error(err))
			return nil, err
		}

		out := make([]models.Market, 0, len(response.GetMarkets()))
		for _, market := range response.GetMarkets() {
			mappedMarket, err := mapper.MarketFromProto(market)
			if err != nil {
				zapLogger.Error(ctx, "ViewMarkets failed", zap.Error(err))
				return nil, err
			}
			out = append(out, mappedMarket)
		}

		return out, nil
	})

	if err != nil {
		return nil, fmt.Errorf("circuit breaker: %w", err)
	}

	return markets, nil
}
