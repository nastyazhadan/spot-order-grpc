package grpc

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	models2 "github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/breaker"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/mapper"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type SpotClient struct {
	api                  proto.SpotInstrumentServiceClient
	viewMarketsBreaker   *gobreaker.CircuitBreaker[*proto.ViewMarketsResponse]
	getMarketByIDBreaker *gobreaker.CircuitBreaker[*proto.GetMarketByIDResponse]
}

func NewSpotClient(connection *grpc.ClientConn, cfg config.CircuitBreakerConfig, logger *zapLogger.Logger) *SpotClient {
	return &SpotClient{
		api:                  proto.NewSpotInstrumentServiceClient(connection),
		viewMarketsBreaker:   breaker.New[*proto.ViewMarketsResponse]("spotService.ViewMarkets", cfg, logger),
		getMarketByIDBreaker: breaker.New[*proto.GetMarketByIDResponse]("spotService.GetMarketByID", cfg, logger),
	}
}

func (c *SpotClient) ViewMarkets(ctx context.Context, roles []models.UserRole) ([]models2.Market, error) {
	userRoles := make([]proto.UserRole, 0, len(roles))
	for _, role := range roles {
		userRoles = append(userRoles, mapper.UserRoleToProto(role))
	}

	response, err := c.viewMarketsBreaker.Execute(func() (*proto.ViewMarketsResponse, error) {
		resp, err := c.api.ViewMarkets(ctx, &proto.ViewMarketsRequest{
			UserRoles: userRoles,
		})
		if err != nil {
			return nil, err
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]models2.Market, 0, len(response.GetMarkets()))
	for _, market := range response.GetMarkets() {
		mappedMarket, mapError := mapper.MarketFromProto(market)
		if mapError != nil {
			return nil, fmt.Errorf("map market from proto: %w", mapError)
		}
		out = append(out, mappedMarket)
	}

	return out, nil
}

func (c *SpotClient) GetMarketByID(ctx context.Context, id uuid.UUID) (models2.Market, error) {
	response, err := c.getMarketByIDBreaker.Execute(func() (*proto.GetMarketByIDResponse, error) {
		return c.api.GetMarketByID(ctx, &proto.GetMarketByIDRequest{
			MarketId:  id.String(),
			UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
		})
	})
	if err != nil {
		return models2.Market{}, err
	}

	market, err := mapper.MarketFromProto(response.GetMarket())
	if err != nil {
		return models2.Market{}, fmt.Errorf("map market from proto: %w", err)
	}

	return market, nil
}
