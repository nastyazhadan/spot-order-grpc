package order

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/mocks"
	protoCommon "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/common/v1"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

func newOrderServer(svc *mocks.OrderService) *serverAPI {
	return &serverAPI{
		service: svc,
		logger:  zapLogger.NewNop(),
	}
}

func ctxWithUserID(userID uuid.UUID) context.Context {
	ctx, _ := requestctx.ContextWithUserID(context.Background(), userID)
	return ctx
}

func dec(v string) *decimal.Decimal {
	return &decimal.Decimal{Value: v}
}

func assertGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "ожидался gRPC status error, получен: %T: %v", err, err)
	assert.Equal(t, want, st.Code())
}

func TestCreateOrder(t *testing.T) {
	validUserID := uuid.New()
	validMarketID := uuid.New()
	validOrderID := uuid.New()

	tests := []struct {
		name       string
		ctx        context.Context
		request    *proto.CreateOrderRequest
		setupMocks func(*mocks.OrderService)
		checkResp  func(t *testing.T, resp *proto.CreateOrderResponse)
		checkErr   func(t *testing.T, err error)
	}{
		{
			name:       "nil request — InvalidArgument",
			ctx:        ctxWithUserID(validUserID),
			request:    nil,
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "market_id пустой — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  "",
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "order_type UNSPECIFIED — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_UNSPECIFIED,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "quantity=0 — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  0,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "quantity отрицательный — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  -5,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "price nil — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     nil,
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "price — невалидная строка — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("not-a-number"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "price=0 — InvalidArgument (не позитивная)",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("0"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "price отрицательная — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("-1.00"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "price превышает допустимую precision — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("1234567890.123456789"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "price на границе допустимой precision — OK",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("1234567890.12345678"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("1234567890.12345678")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(validOrderID, shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, validOrderID.String(), resp.GetOrderId())
			},
		},
		{
			name: "market_id невалидный UUID — InvalidArgument",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  "not-a-uuid",
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name: "нет user_id в контексте — Unauthenticated",
			ctx:  context.Background(),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.Unauthenticated)
			},
		},
		{
			name: "TYPE_LIMIT — маппится в OrderTypeLimit",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  5,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(5),
				).Return(validOrderID, shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				require.NotNil(t, resp)
			},
		},
		{
			name: "TYPE_MARKET — маппится в OrderTypeMarket",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_MARKET,
				Price:     dec("50.00"),
				Quantity:  3,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("50.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeMarket, price, int64(3),
				).Return(validOrderID, shared.OrderStatusPending, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, protoCommon.OrderStatus_STATUS_PENDING, resp.GetStatus())
			},
		},
		{
			name: "сервис возвращает StatusCreated — ответ STATUS_CREATED",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(validOrderID, shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, validOrderID.String(), resp.GetOrderId())
				assert.Equal(t, protoCommon.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
		{
			name: "сервис возвращает StatusPending — ответ STATUS_PENDING",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_MARKET,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeMarket, price, int64(10),
				).Return(validOrderID, shared.OrderStatusPending, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Equal(t, protoCommon.OrderStatus_STATUS_PENDING, resp.GetStatus())
			},
		},
		{
			name: "сервис возвращает serviceErrors.ErrMarketNotFound — пробрасывается",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(uuid.Nil, shared.OrderStatusUnspecified,
					sharedErrors.ErrMarketNotFound{ID: validMarketID})
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrMarketNotFound
				assert.ErrorAs(t, err, &notFound)
			},
		},
		{
			name: "сервис возвращает serviceErrors.ErrMarketDisabled — пробрасывается",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(uuid.Nil, shared.OrderStatusUnspecified,
					serviceErrors.ErrDisabled{ID: validMarketID})
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var disabled serviceErrors.ErrDisabled
				assert.ErrorAs(t, err, &disabled)
				assert.Equal(t, validMarketID, disabled.ID)
			},
		},
		{
			name: "сервис возвращает serviceErrors.ErrRateLimitExceeded — пробрасывается",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(uuid.Nil, shared.OrderStatusUnspecified, serviceErrors.ErrRateLimitExceeded)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorIs(t, err, serviceErrors.ErrRateLimitExceeded)
			},
		},
		{
			name: "сервис возвращает serviceErrors.ErrOrderAlreadyExists — пробрасывается",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(uuid.Nil, shared.OrderStatusUnspecified, serviceErrors.ErrOrderAlreadyExists)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorIs(t, err, serviceErrors.ErrOrderAlreadyExists)
			},
		},
		{
			name: "сервис возвращает gRPC status error — пробрасывается как есть",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(uuid.Nil, shared.OrderStatusUnspecified,
					status.Error(codes.Unavailable, "circuit breaker open"))
			},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.Unavailable)
			},
		},
		{
			name: "сервис возвращает неизвестную ошибку — пробрасывается",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100.00"),
				Quantity:  10,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100.00")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(10),
				).Return(uuid.Nil, shared.OrderStatusUnspecified, errors.New("db timeout"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "db timeout")
			},
		},
		{
			name: "userID из контекста передаётся в сервис без изменений",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("1.00"),
				Quantity:  1,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("1.00")
				svc.On("CreateOrder", mock.Anything,
					validUserID,
					validMarketID,
					shared.OrderTypeLimit, price, int64(1),
				).Return(validOrderID, shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				require.NotNil(t, resp)
			},
		},
		{
			name: "price с ведущими нулями — корректно парсится",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("0.00100000"),
				Quantity:  1,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("0.00100000")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(1),
				).Return(validOrderID, shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				require.NotNil(t, resp)
			},
		},
		{
			name: "price целое число — OK",
			ctx:  ctxWithUserID(validUserID),
			request: &proto.CreateOrderRequest{
				MarketId:  validMarketID.String(),
				OrderType: protoCommon.OrderType_TYPE_LIMIT,
				Price:     dec("100"),
				Quantity:  1,
			},
			setupMocks: func(svc *mocks.OrderService) {
				price, _ := shared.NewDecimal("100")
				svc.On("CreateOrder", mock.Anything, validUserID, validMarketID,
					shared.OrderTypeLimit, price, int64(1),
				).Return(validOrderID, shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.CreateOrderResponse) {
				require.NotNil(t, resp)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mocks.NewOrderService(t)
			tt.setupMocks(svc)

			server := newOrderServer(svc)
			resp, err := server.CreateOrder(tt.ctx, tt.request)

			if tt.checkErr != nil {
				tt.checkErr(t, err)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

func TestGetOrderStatus(t *testing.T) {
	validUserID := uuid.New()
	validOrderID := uuid.New()

	tests := []struct {
		name       string
		ctx        context.Context
		request    *proto.GetOrderStatusRequest
		setupMocks func(*mocks.OrderService)
		checkResp  func(t *testing.T, resp *proto.GetOrderStatusResponse)
		checkErr   func(t *testing.T, err error)
	}{
		{
			name:       "nil request — InvalidArgument",
			ctx:        ctxWithUserID(validUserID),
			request:    nil,
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:       "order_id пустой — InvalidArgument",
			ctx:        ctxWithUserID(validUserID),
			request:    &proto.GetOrderStatusRequest{OrderId: ""},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:       "order_id невалидный UUID — InvalidArgument",
			ctx:        ctxWithUserID(validUserID),
			request:    &proto.GetOrderStatusRequest{OrderId: "not-a-uuid"},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:       "order_id с лишними символами — InvalidArgument",
			ctx:        ctxWithUserID(validUserID),
			request:    &proto.GetOrderStatusRequest{OrderId: validOrderID.String() + "-extra"},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.InvalidArgument)
			},
		},
		{
			name:       "нет user_id в контексте — Unauthenticated",
			ctx:        context.Background(),
			request:    &proto.GetOrderStatusRequest{OrderId: validOrderID.String()},
			setupMocks: func(_ *mocks.OrderService) {},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.Unauthenticated)
			},
		},
		{
			name:    "StatusCreated — STATUS_CREATED",
			ctx:     ctxWithUserID(validUserID),
			request: &proto.GetOrderStatusRequest{OrderId: validOrderID.String()},
			setupMocks: func(svc *mocks.OrderService) {
				svc.On("GetOrderStatus", mock.Anything, validOrderID, validUserID).
					Return(shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, protoCommon.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
		{
			name:    "StatusPending — STATUS_PENDING",
			ctx:     ctxWithUserID(validUserID),
			request: &proto.GetOrderStatusRequest{OrderId: validOrderID.String()},
			setupMocks: func(svc *mocks.OrderService) {
				svc.On("GetOrderStatus", mock.Anything, validOrderID, validUserID).
					Return(shared.OrderStatusPending, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Equal(t, protoCommon.OrderStatus_STATUS_PENDING, resp.GetStatus())
			},
		},
		{
			name:    "orderID и userID из контекста передаются в сервис без изменений",
			ctx:     ctxWithUserID(validUserID),
			request: &proto.GetOrderStatusRequest{OrderId: validOrderID.String()},
			setupMocks: func(svc *mocks.OrderService) {
				svc.On("GetOrderStatus", mock.Anything,
					validOrderID,
					validUserID,
				).Return(shared.OrderStatusCreated, nil)
			},
			checkResp: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				require.NotNil(t, resp)
			},
		},
		{
			name:    "сервис возвращает ErrOrderNotFound — пробрасывается",
			ctx:     ctxWithUserID(validUserID),
			request: &proto.GetOrderStatusRequest{OrderId: validOrderID.String()},
			setupMocks: func(svc *mocks.OrderService) {
				svc.On("GetOrderStatus", mock.Anything, validOrderID, validUserID).
					Return(shared.OrderStatusUnspecified,
						sharedErrors.ErrNotFound{ID: validOrderID})
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrNotFound
				assert.ErrorAs(t, err, &notFound)
				assert.Equal(t, validOrderID, notFound.ID)
			},
		},
		{
			name:    "сервис возвращает gRPC status error — пробрасывается как есть",
			ctx:     ctxWithUserID(validUserID),
			request: &proto.GetOrderStatusRequest{OrderId: validOrderID.String()},
			setupMocks: func(svc *mocks.OrderService) {
				svc.On("GetOrderStatus", mock.Anything, validOrderID, validUserID).
					Return(shared.OrderStatusUnspecified,
						status.Error(codes.Internal, "internal error"))
			},
			checkErr: func(t *testing.T, err error) {
				assertGRPCCode(t, err, codes.Internal)
			},
		},
		{
			name:    "сервис возвращает неизвестную ошибку — пробрасывается",
			ctx:     ctxWithUserID(validUserID),
			request: &proto.GetOrderStatusRequest{OrderId: validOrderID.String()},
			setupMocks: func(svc *mocks.OrderService) {
				svc.On("GetOrderStatus", mock.Anything, validOrderID, validUserID).
					Return(shared.OrderStatusUnspecified, errors.New("pg connection lost"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "pg connection lost")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mocks.NewOrderService(t)
			tt.setupMocks(svc)

			server := newOrderServer(svc)
			resp, err := server.GetOrderStatus(tt.ctx, tt.request)

			if tt.checkErr != nil {
				tt.checkErr(t, err)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

func TestValidatePrice(t *testing.T) {
	tests := []struct {
		name     string
		request  *proto.CreateOrderRequest
		wantCode codes.Code
		wantErr  bool
	}{
		{name: "nil request — InvalidArgument", request: nil, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price nil — InvalidArgument", request: &proto.CreateOrderRequest{Price: nil}, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price пустая строка — InvalidArgument", request: &proto.CreateOrderRequest{Price: dec("")}, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price невалидная строка — InvalidArgument", request: &proto.CreateOrderRequest{Price: dec("abc")}, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price=0 — InvalidArgument", request: &proto.CreateOrderRequest{Price: dec("0")}, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price отрицательная — InvalidArgument", request: &proto.CreateOrderRequest{Price: dec("-0.01")}, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price слишком много дробных знаков — InvalidArgument", request: &proto.CreateOrderRequest{Price: dec("1.123456789")}, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price слишком много целых знаков — InvalidArgument", request: &proto.CreateOrderRequest{Price: dec("12345678901.0")}, wantErr: true, wantCode: codes.InvalidArgument},
		{name: "price валидная — OK", request: &proto.CreateOrderRequest{Price: dec("99999.99999999")}, wantErr: false},
		{name: "price минимально допустимая — OK", request: &proto.CreateOrderRequest{Price: dec("0.00000001")}, wantErr: false},
		{name: "price целое число — OK", request: &proto.CreateOrderRequest{Price: dec("1000000000")}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validatePrice(tt.request)
			if tt.wantErr {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.wantCode, st.Code())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateCreateRequest(t *testing.T) {
	validMarketID := uuid.New().String()

	tests := []struct {
		name     string
		request  *proto.CreateOrderRequest
		wantCode codes.Code
		wantErr  bool
	}{
		{name: "nil request — InvalidArgument", request: nil, wantErr: true, wantCode: codes.InvalidArgument},
		{
			name: "market_id пустой — InvalidArgument",
			request: &proto.CreateOrderRequest{
				MarketId: "", OrderType: protoCommon.OrderType_TYPE_LIMIT, Quantity: 1,
			},
			wantErr: true, wantCode: codes.InvalidArgument,
		},
		{
			name: "order_type UNSPECIFIED — InvalidArgument",
			request: &proto.CreateOrderRequest{
				MarketId: validMarketID, OrderType: protoCommon.OrderType_TYPE_UNSPECIFIED, Quantity: 1,
			},
			wantErr: true, wantCode: codes.InvalidArgument,
		},
		{
			name: "quantity=0 — InvalidArgument",
			request: &proto.CreateOrderRequest{
				MarketId: validMarketID, OrderType: protoCommon.OrderType_TYPE_LIMIT, Quantity: 0,
			},
			wantErr: true, wantCode: codes.InvalidArgument,
		},
		{
			name: "quantity отрицательный — InvalidArgument",
			request: &proto.CreateOrderRequest{
				MarketId: validMarketID, OrderType: protoCommon.OrderType_TYPE_LIMIT, Quantity: -1,
			},
			wantErr: true, wantCode: codes.InvalidArgument,
		},
		{
			name: "всё валидно — OK",
			request: &proto.CreateOrderRequest{
				MarketId: validMarketID, OrderType: protoCommon.OrderType_TYPE_MARKET, Quantity: 100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateRequest(tt.request)
			if tt.wantErr {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.wantCode, st.Code())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
