package grpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/breaker"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/mapper"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
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

func (c *SpotClient) ViewMarkets(ctx context.Context, roles []models.UserRole) ([]models.Market, error) {
	if len(roles) == 0 {
		return nil, serviceErrors.ErrUserRoleNotSpecified
	}

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
		return nil, mapViewMarketsError(err)
	}

	out := make([]models.Market, 0, len(response.GetMarkets()))
	for _, market := range response.GetMarkets() {
		mappedMarket, mapError := mapper.MarketFromProto(market)
		if mapError != nil {
			return nil, fmt.Errorf("map market from proto: %w", mapError)
		}
		out = append(out, mappedMarket)
	}

	return out, nil
}

func (c *SpotClient) GetMarketByID(ctx context.Context, id uuid.UUID) (models.Market, error) {
	response, err := c.getMarketByIDBreaker.Execute(func() (*proto.GetMarketByIDResponse, error) {
		return c.api.GetMarketByID(ctx, &proto.GetMarketByIDRequest{
			MarketId:  id.String(),
			UserRoles: []proto.UserRole{proto.UserRole_ROLE_USER},
		})
	})
	if err != nil {
		return models.Market{}, mapGetMarketByIDError(err, id)
	}

	market, err := mapper.MarketFromProto(response.GetMarket())
	if err != nil {
		return models.Market{}, fmt.Errorf("map market from proto: %w", err)
	}

	return market, nil
}

func mapViewMarketsError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled),
		errors.Is(err, gobreaker.ErrOpenState),
		errors.Is(err, gobreaker.ErrTooManyRequests):
		return serviceErrors.ErrMarketsUnavailable
	}

	stat, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("spot client view markets: %w", err)
	}

	switch stat.Code() {
	case codes.NotFound:
		return serviceErrors.ErrMarketsNotFound
	case codes.Unavailable,
		codes.DeadlineExceeded:
		return serviceErrors.ErrMarketsUnavailable
	default:
		return fmt.Errorf("spot client view markets: %w", err)
	}
}

func mapGetMarketByIDError(err error, marketID uuid.UUID) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled),
		errors.Is(err, gobreaker.ErrOpenState),
		errors.Is(err, gobreaker.ErrTooManyRequests):
		return serviceErrors.ErrUnavailable{ID: marketID}
	}

	stat, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("spot client get market by id: %w", err)
	}

	switch stat.Code() {
	case codes.NotFound:
		return sharedErrors.ErrMarketNotFound{ID: marketID}
	case codes.Unavailable,
		codes.DeadlineExceeded:
		return serviceErrors.ErrUnavailable{ID: marketID}
	default:
		return fmt.Errorf("spot client get market by id: %w", err)
	}
}
