package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	dto "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/inbound"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/auth"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const EmptyValue = 0

type OrderService interface {
	CreateOrder(ctx context.Context,
		userID uuid.UUID,
		marketID uuid.UUID,
		orderType models.OrderType,
		price sharedModels.Decimal,
		quantity int64,
	) (uuid.UUID, models.OrderStatus, error)

	GetOrderStatus(ctx context.Context,
		orderId, userID uuid.UUID,
	) (models.OrderStatus, error)
}

type serverAPI struct {
	proto.UnimplementedOrderServiceServer
	service OrderService
}

func Register(grpc *grpc.Server, service OrderService) {
	proto.RegisterOrderServiceServer(grpc, &serverAPI{
		service: service,
	})
}

func (s *serverAPI) CreateOrder(
	ctx context.Context,
	request *proto.CreateOrderRequest,
) (*proto.CreateOrderResponse, error) {
	if err := validateErrors(request); err != nil {
		return nil, err
	}

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user_id not found in token")
	}
	marketID, err := uuid.Parse(request.GetMarketId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("market_id must be a valid UUID"))
	}

	orderType := dto.TypeFromProto(request.GetOrderType())
	orderPrice := sharedModels.NewDecimal(request.GetPrice().GetValue())
	orderQuantity := request.GetQuantity()

	orderID, orderStatus, err := s.service.CreateOrder(
		ctx, userID, marketID, orderType, orderPrice, orderQuantity,
	)
	if err != nil {
		if !errors.Is(err, serviceErrors.ErrMarketsNotFound) &&
			!errors.Is(err, serviceErrors.ErrOrderAlreadyExists) &&
			!errors.Is(err, serviceErrors.ErrCreatingOrderNotRequired) &&
			!errors.Is(err, serviceErrors.ErrRateLimitExceeded) {
			zapLogger.Error(ctx, "failed to create order",
				zap.String("user_id", userID.String()),
				zap.String("market_id", marketID.String()),
				zap.Error(err),
			)
		}
		return nil, err
	}

	return &proto.CreateOrderResponse{
		OrderId: orderID.String(),
		Status:  dto.StatusToProto(orderStatus),
	}, nil

}

func (s *serverAPI) GetOrderStatus(
	ctx context.Context,
	request *proto.GetOrderStatusRequest,
) (*proto.GetOrderStatusResponse, error) {
	if request.GetOrderId() == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}

	orderID, err := uuid.Parse(request.GetOrderId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("user_id must be a valid UUID"))
	}
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user_id not found in token")
	}

	orderStatus, err := s.service.GetOrderStatus(ctx, orderID, userID)
	if err != nil {
		if !errors.Is(err, serviceErrors.ErrOrderNotFound) {
			zapLogger.Error(ctx, "failed to get order status",
				zap.String("order_id", orderID.String()),
				zap.String("user_id", userID.String()),
				zap.Error(err),
			)
		}
		return nil, err
	}

	return &proto.GetOrderStatusResponse{
		Status: dto.StatusToProto(orderStatus),
	}, nil
}

func validateErrors(request *proto.CreateOrderRequest) error {
	if request.GetMarketId() == "" {
		return status.Error(codes.InvalidArgument, "market_id is required")
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
