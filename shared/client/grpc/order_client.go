package grpc

import (
	"context"
	"fmt"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type OrderClient struct {
	api            proto.OrderServiceClient
	circuitBreaker *gobreaker.CircuitBreaker[any]
}

func NewOrderClient(connection *grpc.ClientConn, cfg config.CircuitBreakerConfig) *OrderClient {
	circuitBreaker := gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name:        "orderService",
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			shouldTrip := counts.ConsecutiveFailures >= cfg.MaxFailures

			if shouldTrip {
				zapLogger.Warn(context.Background(), "circuit breaker is about to open",
					zap.String("name", "orderService"),
					zap.Uint32("consecutive_failures", counts.ConsecutiveFailures),
					zap.Uint32("total_failures", counts.TotalFailures),
					zap.Uint32("total_requests", counts.Requests),
				)
			}

			return shouldTrip
		},
	})

	return &OrderClient{
		api:            proto.NewOrderServiceClient(connection),
		circuitBreaker: circuitBreaker,
	}
}

func (c *OrderClient) CreateOrder(
	ctx context.Context,
	req *proto.CreateOrderRequest,
) (*proto.CreateOrderResponse, error) {

	result, err := c.circuitBreaker.Execute(func() (any, error) {
		return c.api.CreateOrder(ctx, req)
	})
	if err != nil {
		return nil, fmt.Errorf("circuit breaker: %w", err)
	}

	return result.(*proto.CreateOrderResponse), nil
}

func (c *OrderClient) GetOrderStatus(
	ctx context.Context,
	req *proto.GetOrderStatusRequest,
) (*proto.GetOrderStatusResponse, error) {

	result, err := c.circuitBreaker.Execute(func() (any, error) {
		return c.api.GetOrderStatus(ctx, req)
	})
	if err != nil {
		return nil, fmt.Errorf("circuit breaker: %w", err)
	}

	return result.(*proto.GetOrderStatusResponse), nil
}
