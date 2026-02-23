package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/mapper"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v6"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const EmptyValue = 0

type Order interface {
	CreateOrder(ctx context.Context,
		userID uuid.UUID,
		marketID uuid.UUID,
		orderType models.OrderType,
		price models.Decimal,
		quantity int64,
	) (uuid.UUID, models.OrderStatus, error)

	GetOrderStatus(ctx context.Context,
		orderId, userID uuid.UUID,
	) (models.OrderStatus, error)
}

type serverAPI struct {
	proto.UnimplementedOrderServiceServer
	service Order
}

func Register(grpc *grpc.Server, service Order) {
	proto.RegisterOrderServiceServer(grpc, &serverAPI{
		service: service,
	})
}

func (s *serverAPI) CreateOrder(
	ctx context.Context,
	request *proto.CreateOrderRequest,
) (*proto.CreateOrderResponse, error) {
	if err := validateCreateError(request); err != nil {
		return nil, err
	}

	userID, err := mustParseUUID(request.GetUserId())
	if err != nil {
		return nil, err
	}
	marketID, err := mustParseUUID(request.GetMarketId())
	if err != nil {
		return nil, err
	}

	orderType := mapper.TypeFromProto(request.GetOrderType())
	orderPrice := request.GetPrice()
	orderQuantity := request.GetQuantity()

	orderID, orderStatus, err := s.service.CreateOrder(ctx,
		userID,
		marketID,
		orderType,
		orderPrice,
		orderQuantity,
	)

	if err != nil {
		if !errors.Is(err, serviceErrors.ErrMarketsNotFound) &&
			!errors.Is(err, serviceErrors.ErrOrderAlreadyExists) &&
			!errors.Is(err, serviceErrors.ErrCreatingOrderNotRequired) {
			zapLogger.Error(ctx, "failed to create order",
				zap.String("userId", userID.String()),
				zap.Error(err),
			)
		}

		return nil, validateServiceError(err)
	}

	return &proto.CreateOrderResponse{
		OrderId: orderID.String(),
		Status:  mapper.StatusToProto(orderStatus),
	}, nil

}

func (s *serverAPI) GetOrderStatus(
	ctx context.Context,
	request *proto.GetOrderStatusRequest,
) (*proto.GetOrderStatusResponse, error) {
	if request.GetOrderId() == "" || request.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id and user_id are required")
	}

	orderID, err := mustParseUUID(request.GetOrderId())
	if err != nil {
		return nil, err
	}
	userID, err := mustParseUUID(request.GetUserId())
	if err != nil {
		return nil, err
	}

	orderStatus, err := s.service.GetOrderStatus(ctx, orderID, userID)
	if err != nil {
		if !errors.Is(err, serviceErrors.ErrOrderNotFound) {
			zapLogger.Error(ctx, "failed to get order status",
				zap.String("orderId", orderID.String()),
				zap.String("userId", userID.String()),
				zap.Error(err),
			)
		}

		return nil, validateServiceError(err)
	}

	return &proto.GetOrderStatusResponse{
		Status: mapper.StatusToProto(orderStatus),
	}, nil
}

func validateCreateError(request *proto.CreateOrderRequest) error {
	if request.GetUserId() == "" || request.GetMarketId() == "" {
		return status.Error(codes.InvalidArgument, "user_id and market_id are required")
	}

	if request.GetOrderType() == proto.OrderType_TYPE_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, "order_type is required")
	}

	if request.GetPrice() == nil || request.GetPrice().GetValue() == "" {
		return status.Error(codes.InvalidArgument, "price is required")
	}

	price, err := strconv.ParseFloat(request.GetPrice().GetValue(), 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) {
		return status.Error(codes.InvalidArgument, "price must be a number")
	}

	if price <= EmptyValue {
		return status.Error(codes.InvalidArgument, "price must be > 0")
	}

	if request.GetQuantity() <= EmptyValue {
		return status.Error(codes.InvalidArgument, "quantity must be > 0")
	}

	return nil
}

func validateServiceError(err error) error {
	switch {
	case errors.Is(err, serviceErrors.ErrCreatingOrderNotRequired):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, serviceErrors.ErrOrderAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, serviceErrors.ErrMarketsNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, serviceErrors.ErrOrderNotFound):
		return status.Error(codes.NotFound, err.Error())

	default:
		return status.Error(codes.Internal, "internal error")
	}
}

func mustParseUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, status.Error(codes.InvalidArgument, fmt.Sprintf("%q must be a valid UUID", s))
	}

	return id, nil
}
