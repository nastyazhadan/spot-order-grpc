package order

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/mapper"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v6"

	"github.com/google/uuid"
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

func (server *serverAPI) CreateOrder(
	ctx context.Context,
	request *proto.CreateOrderRequest,
) (*proto.CreateOrderResponse, error) {
	if err := validateErrorCreate(request); err != nil {
		return nil, err
	}

	userID, _ := uuid.Parse(request.GetUserId())
	marketID, _ := uuid.Parse(request.GetMarketId())
	orderType := mapper.TypeFromProto(request.GetOrderType())
	orderPrice := request.GetPrice()
	orderQuantity := request.GetQuantity()

	orderID, orderStatus, err := server.service.CreateOrder(ctx,
		userID,
		marketID,
		orderType,
		orderPrice,
		orderQuantity,
	)

	if err != nil {
		serviceError := validateServiceError(err)
		return nil, serviceError
	}

	return &proto.CreateOrderResponse{
		OrderId: orderID.String(),
		Status:  mapper.StatusToProto(orderStatus),
	}, nil

}

func (server *serverAPI) GetOrderStatus(
	ctx context.Context,
	request *proto.GetOrderStatusRequest,
) (*proto.GetOrderStatusResponse, error) {
	if err := validateErrorGet(request); err != nil {
		return nil, err
	}

	orderID, _ := uuid.Parse(request.GetOrderId())
	userID, _ := uuid.Parse(request.GetUserId())
	orderStatus, err := server.service.GetOrderStatus(ctx, orderID, userID)

	if err != nil {
		serviceError := validateServiceError(err)
		return nil, serviceError
	}

	return &proto.GetOrderStatusResponse{
		Status: mapper.StatusToProto(orderStatus),
	}, nil
}

func validateErrorCreate(request *proto.CreateOrderRequest) error {
	if request.GetUserId() == "" || request.GetMarketId() == "" {
		return status.Error(codes.InvalidArgument, "user_id and market_id are required")
	}

	if _, err := uuid.Parse(request.GetUserId()); err != nil {
		return status.Error(codes.InvalidArgument, "user_id must be UUID")
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

func validateErrorGet(request *proto.GetOrderStatusRequest) error {
	if request.GetOrderId() == "" || request.GetUserId() == "" {
		return status.Error(codes.InvalidArgument, "order_id and user_id are required")
	}

	_, err := uuid.Parse(request.GetOrderId())
	if err != nil {
		return status.Error(codes.InvalidArgument, "order_id must be UUID")
	}

	_, err = uuid.Parse(request.GetUserId())
	if err != nil {
		return status.Error(codes.InvalidArgument, "user_id must be UUID")
	}

	return nil
}
