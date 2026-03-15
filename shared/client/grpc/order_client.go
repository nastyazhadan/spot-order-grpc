package grpc

import (
	"context"
	"fmt"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type OrderClient struct {
	api                   proto.OrderServiceClient
	createOrderBreaker    *gobreaker.CircuitBreaker[*proto.CreateOrderResponse]
	getOrderStatusBreaker *gobreaker.CircuitBreaker[*proto.GetOrderStatusResponse]
}

func NewOrderClient(connection *grpc.ClientConn, cfg config.CircuitBreakerConfig) *OrderClient {
	createBreaker := gobreaker.NewCircuitBreaker[*proto.CreateOrderResponse](gobreaker.Settings{
		Name:        "orderService.CreateOrder",
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			shouldTrip := counts.ConsecutiveFailures >= cfg.MaxFailures

			if shouldTrip {
				zapLogger.Warn(context.Background(), "circuit breaker is about to open",
					zap.String("name", "orderService.CreateOrder"),
					zap.Uint32("consecutive_failures", counts.ConsecutiveFailures),
					zap.Uint32("total_failures", counts.TotalFailures),
					zap.Uint32("total_requests", counts.Requests),
				)
			}

			return shouldTrip
		},
	})

	getBreaker := gobreaker.NewCircuitBreaker[*proto.GetOrderStatusResponse](gobreaker.Settings{
		Name:        "orderService.GetOrderStatus",
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			shouldTrip := counts.ConsecutiveFailures >= cfg.MaxFailures

			if shouldTrip {
				zapLogger.Warn(context.Background(), "circuit breaker is about to open",
					zap.String("name", "orderService.GetOrderStatus"),
					zap.Uint32("consecutive_failures", counts.ConsecutiveFailures),
					zap.Uint32("total_failures", counts.TotalFailures),
					zap.Uint32("total_requests", counts.Requests),
				)
			}

			return shouldTrip
		},
	})

	return &OrderClient{
		api:                   proto.NewOrderServiceClient(connection),
		createOrderBreaker:    createBreaker,
		getOrderStatusBreaker: getBreaker,
	}
}

func (c *OrderClient) CreateOrder(
	ctx context.Context,
	request *proto.CreateOrderRequest,
) (*proto.CreateOrderResponse, error) {

	result, err := c.createOrderBreaker.Execute(func() (*proto.CreateOrderResponse, error) {
		return c.api.CreateOrder(ctx, request)
	})
	if err != nil {
		return nil, fmt.Errorf("circuit breaker: %w", err)
	}

	return result, nil
}

func (c *OrderClient) GetOrderStatus(
	ctx context.Context,
	request *proto.GetOrderStatusRequest,
) (*proto.GetOrderStatusResponse, error) {

	result, err := c.getOrderStatusBreaker.Execute(func() (*proto.GetOrderStatusResponse, error) {
		return c.api.GetOrderStatus(ctx, request)
	})
	if err != nil {
		return nil, fmt.Errorf("circuit breaker: %w", err)
	}

	return result, nil
}
