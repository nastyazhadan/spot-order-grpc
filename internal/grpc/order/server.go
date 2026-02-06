package order

import (
	"context"
	"errors"
	orderSvc "spotOrder/internal/services/order"
	"strconv"

	spot_orderv1 "github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Order interface {
	CreateOrder(ctx context.Context,
		userID, marketID string,
		orderType spot_orderv1.OrderType,
		price *decimal.Decimal,
		quantity int64,
	) (string, spot_orderv1.OrderStatus, error)

	GetOrderStatus(ctx context.Context,
		orderId, userID string,
	) (spot_orderv1.OrderStatus, error)
}

type serverAPI struct {
	spot_orderv1.UnimplementedOrderServiceServer
	svc Order
}

func Register(grpc *grpc.Server, svc Order) {
	spot_orderv1.RegisterOrderServiceServer(grpc, &serverAPI{
		svc: svc,
	})
}

const EmptyValue = 0

func (s *serverAPI) CreateOrder(
	ctx context.Context,
	req *spot_orderv1.CreateOrderRequest,
) (*spot_orderv1.CreateOrderResponse, error) {
	if err := validateError(req); err != nil {
		return nil, err
	}

	orderID, stat, err := s.svc.CreateOrder(ctx,
		req.GetUserId(),
		req.GetMarketId(),
		req.GetOrderType(),
		req.GetPrice(),
		req.GetQuantity(),
	)

	if err != nil {
		switch {
		case errors.Is(err, orderSvc.ErrOrderAlreadyExists):
			return nil, status.Error(codes.AlreadyExists, err.Error())
		case errors.Is(err, orderSvc.ErrMarketNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.Internal, "internal error")

		}
	}

	return &spot_orderv1.CreateOrderResponse{
		OrderId: orderID,
		Status:  stat,
	}, nil

}

func (s *serverAPI) GetOrderStatus(
	ctx context.Context,
	req *spot_orderv1.GetOrderStatusRequest,
) (*spot_orderv1.GetOrderStatusResponse, error) {
	if req.GetOrderId() == "" || req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id and user_id are required")
	}

	stat, err := s.svc.GetOrderStatus(ctx, req.GetOrderId(), req.GetUserId())
	if err != nil {
		switch {
		case errors.Is(err, orderSvc.ErrOrderNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		case errors.Is(err, orderSvc.ErrMarketNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	return &spot_orderv1.GetOrderStatusResponse{
		Status: stat,
	}, nil
}

func validateError(req *spot_orderv1.CreateOrderRequest) error {
	if req.GetUserId() == "" || req.GetMarketId() == "" {
		return status.Error(codes.InvalidArgument, "user_id and market_id are required")
	}
	if req.GetOrderType() == spot_orderv1.OrderType_TYPE_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, "order_type is required")
	}
	if req.GetPrice() == nil || req.GetPrice().GetValue() == "" {
		return status.Error(codes.InvalidArgument, "price is required")
	}

	price, _ := strconv.ParseFloat(req.GetPrice().Value, 64)
	if price <= EmptyValue {
		return status.Error(codes.InvalidArgument, "price must be > 0")
	}

	if req.GetQuantity() <= EmptyValue {
		return status.Error(codes.InvalidArgument, "quantity must be > 0")
	}

	return nil
}
