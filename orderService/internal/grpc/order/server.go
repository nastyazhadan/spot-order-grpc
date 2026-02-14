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
		orderType models.Type,
		price models.Decimal,
		quantity int64,
	) (uuid.UUID, models.Status, error)

	GetOrderStatus(ctx context.Context,
		orderId, userID uuid.UUID,
	) (models.Status, error)
}

type serverAPI struct {
	proto.UnimplementedOrderServiceServer
	svc Order
}

func Register(grpc *grpc.Server, svc Order) {
	proto.RegisterOrderServiceServer(grpc, &serverAPI{
		svc: svc,
	})
}

func (s *serverAPI) CreateOrder(
	ctx context.Context,
	req *proto.CreateOrderRequest,
) (*proto.CreateOrderResponse, error) {
	if err := validateError(req); err != nil {
		return nil, err
	}

	userID, _ := uuid.Parse(req.GetUserId())
	marketID, _ := uuid.Parse(req.GetMarketId())

	orderID, stat, err := s.svc.CreateOrder(ctx,
		userID,
		marketID,
		mapper.TypeFromProto(req.GetOrderType()),
		req.GetPrice(),
		req.GetQuantity(),
	)

	if err != nil {
		switch {
		case errors.Is(err, serviceErrors.ErrOrderAlreadyExists):
			return nil, status.Error(codes.AlreadyExists, err.Error())
		case errors.Is(err, serviceErrors.ErrMarketsNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	return &proto.CreateOrderResponse{
		OrderId: orderID.String(),
		Status:  mapper.StatusToProto(stat),
	}, nil

}

func (s *serverAPI) GetOrderStatus(
	ctx context.Context,
	req *proto.GetOrderStatusRequest,
) (*proto.GetOrderStatusResponse, error) {
	if req.GetOrderId() == "" || req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id and user_id are required")
	}

	orderID, err := uuid.Parse(req.GetOrderId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "order_id must be UUID")
	}

	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "user_id must be UUID")
	}

	stat, err := s.svc.GetOrderStatus(ctx, orderID, userID)
	if err != nil {
		switch {
		case errors.Is(err, serviceErrors.ErrOrderNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	return &proto.GetOrderStatusResponse{
		Status: mapper.StatusToProto(stat),
	}, nil
}

func validateError(req *proto.CreateOrderRequest) error {
	if req.GetUserId() == "" || req.GetMarketId() == "" {
		return status.Error(codes.InvalidArgument, "user_id and market_id are required")
	}
	if _, err := uuid.Parse(req.GetUserId()); err != nil {
		return status.Error(codes.InvalidArgument, "user_id must be UUID")
	}

	if req.GetOrderType() == proto.OrderType_TYPE_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, "order_type is required")
	}
	if req.GetPrice() == nil || req.GetPrice().GetValue() == "" {
		return status.Error(codes.InvalidArgument, "price is required")
	}

	price, err := strconv.ParseFloat(req.GetPrice().GetValue(), 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) {
		return status.Error(codes.InvalidArgument, "price must be a number")
	}
	if price <= EmptyValue {
		return status.Error(codes.InvalidArgument, "price must be > 0")
	}

	if req.GetQuantity() <= EmptyValue {
		return status.Error(codes.InvalidArgument, "quantity must be > 0")
	}

	return nil
}
