package grpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
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
	logger               *zapLogger.Logger
}

func NewSpotClient(connection *grpc.ClientConn, cfg config.CircuitBreakerConfig, logger *zapLogger.Logger) *SpotClient {
	return &SpotClient{
		api:                  proto.NewSpotInstrumentServiceClient(connection),
		viewMarketsBreaker:   breaker.New[*proto.ViewMarketsResponse]("spotService.ViewMarkets", cfg, logger),
		getMarketByIDBreaker: breaker.New[*proto.GetMarketByIDResponse]("spotService.GetMarketByID", cfg, logger),
		logger:               logger,
	}
}

func (c *SpotClient) ViewMarkets(
	ctx context.Context,
	limit, offset uint64,
) ([]models.Market, uint64, bool, error) {
	response, err := c.viewMarketsBreaker.Execute(func() (*proto.ViewMarketsResponse, error) {
		resp, err := c.api.ViewMarkets(ctx, &proto.ViewMarketsRequest{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		return resp, nil
	})
	if err != nil {
		c.logViewMarketsBreakerError(ctx, err, limit, offset)

		return nil, 0, false, mapViewMarketsError(err)
	}

	out := make([]models.Market, 0, len(response.GetMarkets()))
	for _, market := range response.GetMarkets() {
		mappedMarket, mapError := mapper.MarketFromProto(market)
		if mapError != nil {
			return nil, 0, false, fmt.Errorf("map market from proto: %w", mapError)
		}
		out = append(out, mappedMarket)
	}

	return out, response.GetNextOffset(), response.GetHasMore(), nil
}

func (c *SpotClient) GetMarketByID(
	ctx context.Context,
	id uuid.UUID,
) (models.Market, error) {
	response, err := c.getMarketByIDBreaker.Execute(func() (*proto.GetMarketByIDResponse, error) {
		return c.api.GetMarketByID(ctx, &proto.GetMarketByIDRequest{
			MarketId: id.String(),
		})
	})
	if err != nil {
		c.logGetMarketByIDBreakerError(ctx, err, id)

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
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded),
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
		if shouldPassThroughGRPCCode(stat.Code()) {
			return err
		}

		return fmt.Errorf("spot client view markets: %w", err)
	}
}

func mapGetMarketByIDError(err error, marketID uuid.UUID) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded),
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
	case codes.FailedPrecondition:
		return serviceErrors.ErrDisabled{ID: marketID}
	default:
		if shouldPassThroughGRPCCode(stat.Code()) {
			return err
		}

		return fmt.Errorf("spot client get market by id: %w", err)
	}
}

func shouldPassThroughGRPCCode(code codes.Code) bool {
	switch code {
	case codes.ResourceExhausted,
		codes.Unauthenticated,
		codes.PermissionDenied:
		return true
	default:
		return false
	}
}

func (c *SpotClient) logViewMarketsBreakerError(
	ctx context.Context,
	err error,
	limit, offset uint64,
) {
	fields := []zap.Field{
		zap.Uint64("limit", limit),
		zap.Uint64("offset", offset),
		zap.Error(err),
	}

	switch {
	case errors.Is(err, gobreaker.ErrOpenState):
		c.logger.Warn(ctx, "SpotClient.ViewMarkets skipped by open circuit breaker", fields...)
	case errors.Is(err, gobreaker.ErrTooManyRequests):
		c.logger.Warn(ctx, "SpotClient.ViewMarkets rejected by half-open circuit breaker", fields...)
	case errors.Is(err, context.DeadlineExceeded):
		c.logger.Warn(ctx, "SpotClient.ViewMarkets failed by deadline exceeded", fields...)
	case errors.Is(err, context.Canceled):
		c.logger.Info(ctx, "SpotClient.ViewMarkets canceled", fields...)
	default:
		c.logger.Warn(ctx, "SpotClient.ViewMarkets failed", fields...)
	}
}

func (c *SpotClient) logGetMarketByIDBreakerError(
	ctx context.Context,
	err error,
	marketID uuid.UUID,
) {
	fields := []zap.Field{
		zap.String("market_id", marketID.String()),
		zap.Error(err),
	}

	switch {
	case errors.Is(err, gobreaker.ErrOpenState):
		c.logger.Warn(ctx, "SpotClient.GetMarketByID skipped by open circuit breaker", fields...)
	case errors.Is(err, gobreaker.ErrTooManyRequests):
		c.logger.Warn(ctx, "SpotClient.GetMarketByID rejected by half-open circuit breaker", fields...)
	case errors.Is(err, context.DeadlineExceeded):
		c.logger.Warn(ctx, "SpotClient.GetMarketByID failed by deadline exceeded", fields...)
	case errors.Is(err, context.Canceled):
		c.logger.Info(ctx, "SpotClient.GetMarketByID canceled", fields...)
	default:
		c.logger.Warn(ctx, "SpotClient.GetMarketByID failed", fields...)
	}
}
