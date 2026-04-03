package order

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mapper "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/inbound"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	protoCommon "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/common/v1"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/errors"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

const (
	minQuantity    = 0
	pricePrecision = 18
	priceScale     = 8
)

type OrderService interface {
	CreateOrder(ctx context.Context,
		userID uuid.UUID,
		marketID uuid.UUID,
		orderType shared.OrderType,
		price shared.Decimal,
		quantity int64,
	) (uuid.UUID, shared.OrderStatus, error)

	GetOrderStatus(ctx context.Context,
		orderID, userID uuid.UUID,
	) (shared.OrderStatus, error)
}

type serverAPI struct {
	proto.UnimplementedOrderServiceServer
	service OrderService
	logger  *zapLogger.Logger
}

func Register(server *grpc.Server, service OrderService, logger *zapLogger.Logger) {
	proto.RegisterOrderServiceServer(server, &serverAPI{
		service: service,
		logger:  logger,
	})
}

func (s *serverAPI) CreateOrder(
	ctx context.Context,
	request *proto.CreateOrderRequest,
) (*proto.CreateOrderResponse, error) {
	if err := validateCreateRequest(request); err != nil {
		return nil, err
	}

	userID, ok := requestctx.UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user_id not found in token")
	}
	marketID, err := uuid.Parse(request.GetMarketId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "market_id must be a valid UUID")
	}
	orderPrice, err := validatePrice(request)
	if err != nil {
		return nil, err
	}
	orderType := mapper.TypeFromProto(request.GetOrderType())
	orderQuantity := request.GetQuantity()

	ctx = s.logger.WithFields(ctx,
		zap.String("market_id", marketID.String()),
		zap.String("price", orderPrice.String()),
		zap.Int64("quantity", orderQuantity),
	)

	orderID, orderStatus, err := s.service.CreateOrder(
		ctx, userID, marketID, orderType, orderPrice, orderQuantity,
	)
	if err != nil {
		return nil, err
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
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, errors.MsgRequestRequired)
	}
	if request.GetOrderId() == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}

	userID, found := requestctx.UserIDFromContext(ctx)
	if !found {
		return nil, status.Error(codes.Unauthenticated, "user_id not found in token")
	}
	orderID, err := uuid.Parse(request.GetOrderId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "order_id must be a valid UUID")
	}

	ctx = s.logger.WithFields(ctx,
		zap.String("order_id", orderID.String()),
	)

	orderStatus, err := s.service.GetOrderStatus(ctx, orderID, userID)
	if err != nil {
		return nil, err
	}

	return &proto.GetOrderStatusResponse{
		Status: mapper.StatusToProto(orderStatus),
	}, nil
}

func validateCreateRequest(request *proto.CreateOrderRequest) error {
	if request == nil {
		return status.Error(codes.InvalidArgument, errors.MsgRequestRequired)
	}

	if request.GetMarketId() == "" {
		return status.Error(codes.InvalidArgument, "market_id is required")
	}

	if request.GetOrderType() == protoCommon.OrderType_TYPE_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, "order_type is required")
	}

	if request.GetQuantity() <= minQuantity {
		return status.Error(codes.InvalidArgument, "quantity must be > 0")
	}

	return nil
}

func validatePrice(request *proto.CreateOrderRequest) (shared.Decimal, error) {
	price := request.GetPrice()
	if price == nil {
		return shared.Decimal{}, status.Error(codes.InvalidArgument, "price is required")
	}

	validPrice, err := shared.NewDecimal(price.Value)
	if err != nil {
		return shared.Decimal{}, status.Error(codes.InvalidArgument, "price must be a valid decimal number")
	}

	if !validPrice.IsPositive() {
		return shared.Decimal{}, status.Error(codes.InvalidArgument, "price must be > 0")
	}

	if !validPrice.FitsNumeric(pricePrecision, priceScale) {
		return shared.Decimal{}, status.Error(
			codes.InvalidArgument,
			"price must have at most 10 integer digits and 8 fractional digits",
		)
	}

	return validPrice, nil
}
