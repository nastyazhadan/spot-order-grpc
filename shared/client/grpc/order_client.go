package grpc

import (
	"context"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"

	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/client/grpc/breaker"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

type OrderClient struct {
	api                   proto.OrderServiceClient
	createOrderBreaker    *gobreaker.CircuitBreaker[*proto.CreateOrderResponse]
	getOrderStatusBreaker *gobreaker.CircuitBreaker[*proto.GetOrderStatusResponse]
}

func NewOrderClient(
	connection *grpc.ClientConn,
	cfg config.CircuitBreakerConfig,
	logger *zapLogger.Logger,
) *OrderClient {
	return &OrderClient{
		api:                   proto.NewOrderServiceClient(connection),
		createOrderBreaker:    breaker.New[*proto.CreateOrderResponse]("orderService.CreateOrder", cfg, logger),
		getOrderStatusBreaker: breaker.New[*proto.GetOrderStatusResponse]("orderService.GetOrderStatus", cfg, logger),
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
		return nil, err
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
		return nil, err
	}

	return result, nil
}
