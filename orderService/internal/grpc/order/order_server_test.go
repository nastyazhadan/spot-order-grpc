package order

import (
	"context"
	"errors"
	"testing"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/mocks"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	proto "github.com/nastyazhadan/spot-order-grpc/shared/protos/gen/go/order/v6"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateOrder(t *testing.T) {
	gofakeit.Seed(0)

	validUserID := uuid.New().String()
	validMarketID := uuid.New().String()
	validOrderID := uuid.New()

	tests := []struct {
		name          string
		request       *proto.CreateOrderRequest
		setupMocks    func(*mocks.Order)
		expectedErr   error
		expectedCode  codes.Code
		checkResponse func(t *testing.T, resp *proto.CreateOrderResponse)
	}{
		{
			name: "успешное создание ордера",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(10),
				).Return(validOrderID, models.OrderStatusCreated, nil)
			},
			expectedErr: nil,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.NotNil(t, resp)
				assert.NotEmpty(t, resp.GetOrderId())
				assert.Equal(t, proto.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
		{
			name: "валидация - user_id пустой",
			request: &proto.CreateOrderRequest{
				UserId:    "",
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - market_id пустой",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  "",
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - невалидный user_id UUID",
			request: &proto.CreateOrderRequest{
				UserId:    "invalid-uuid",
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - order_type не указан",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_UNSPECIFIED,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - price пустая",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     nil,
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - price значение пустое",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: ""},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - price не число",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "not-a-number"},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - price меньше или равна 0",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "0"},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - price отрицательная",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "-10.50"},
				Quantity:  10,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - quantity меньше или равно 0",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  0,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - quantity отрицательное",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  -5,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - ErrCreatingOrderNotRequired",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(10),
				).Return(uuid.Nil, models.OrderStatusCancelled, serviceErrors.ErrCreatingOrderNotRequired)
			},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - ErrOrderAlreadyExists",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(10),
				).Return(uuid.Nil, models.OrderStatusCancelled, serviceErrors.ErrOrderAlreadyExists)
			},
			expectedCode: codes.AlreadyExists,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - ErrMarketsNotFound",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(10),
				).Return(uuid.Nil, models.OrderStatusCancelled, serviceErrors.ErrMarketsNotFound)
			},
			expectedCode: codes.NotFound,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - внутренняя ошибка",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "100.50"},
				Quantity:  10,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(10),
				).Return(uuid.Nil, models.OrderStatusCancelled, errors.New("database error"))
			},
			expectedCode: codes.Internal,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "успешное создание с разными типами ордеров - LIMIT",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_LIMIT,
				Price:     &decimal.Decimal{Value: "150.75"},
				Quantity:  25,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(25),
				).Return(validOrderID, models.OrderStatusCreated, nil)
			},
			expectedErr: nil,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.NotNil(t, resp)
				assert.NotEmpty(t, resp.GetOrderId())
				assert.Equal(t, proto.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
		{
			name: "corner case - минимальная цена",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "0.01"},
				Quantity:  1,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(1),
				).Return(validOrderID, models.OrderStatusCreated, nil)
			},
			expectedErr: nil,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.NotNil(t, resp)
				assert.Equal(t, proto.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
		{
			name: "corner case - очень большая цена",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "999999.99"},
				Quantity:  1,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("CreateOrder",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("models.OrderType"),
					mock.Anything,
					int64(1),
				).Return(validOrderID, models.OrderStatusCreated, nil)
			},
			expectedErr: nil,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.NotNil(t, resp)
				assert.Equal(t, proto.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			mockOrder := new(mocks.Order)
			tt.setupMocks(mockOrder)

			server := &serverAPI{
				service: mockOrder,
			}

			ctx := context.Background()

			resp, err := server.CreateOrder(ctx, tt.request)

			if tt.expectedCode != codes.OK {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, tt.expectedCode, st.Code())
			} else {
				require.NoError(t, err)
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}

			mockOrder.AssertExpectations(t)
		})
	}
}

func TestGetOrderStatus(t *testing.T) {
	gofakeit.Seed(0)

	validUserID := uuid.New().String()
	validOrderID := uuid.New().String()

	tests := []struct {
		name          string
		request       *proto.GetOrderStatusRequest
		setupMocks    func(*mocks.Order)
		expectedCode  codes.Code
		checkResponse func(t *testing.T, resp *proto.GetOrderStatusResponse)
	}{
		{
			name: "успешное получение статуса",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusCreated, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.NotNil(t, resp)
				assert.Equal(t, proto.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
		{
			name: "валидация - order_id пустой",
			request: &proto.GetOrderStatusRequest{
				OrderId: "",
				UserId:  validUserID,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - user_id пустой",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  "",
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - невалидный order_id UUID",
			request: &proto.GetOrderStatusRequest{
				OrderId: "invalid-uuid",
				UserId:  validUserID,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - невалидный user_id UUID",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  "invalid-uuid",
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - ErrOrderNotFound",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusUnspecified, serviceErrors.ErrOrderNotFound)
			},
			expectedCode: codes.NotFound,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "ошибка сервиса - внутренняя ошибка",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusUnspecified, errors.New("database error"))
			},
			expectedCode: codes.Internal,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "успешное получение - статус Created",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusCreated, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.NotNil(t, resp)
				assert.Equal(t, proto.OrderStatus_STATUS_CREATED, resp.GetStatus())
			},
		},
		{
			name: "успешное получение - статус Cancelled",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusCancelled, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.NotNil(t, resp)
				assert.Equal(t, proto.OrderStatus_STATUS_CANCELLED, resp.GetStatus())
			},
		},
		{
			name: "успешное получение - статус Pending",
			request: &proto.GetOrderStatusRequest{
				OrderId: validOrderID,
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusPending, nil)
			},
			expectedCode: codes.OK,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.NotNil(t, resp)
				assert.Equal(t, proto.OrderStatus_STATUS_PENDING, resp.GetStatus())
			},
		},
		{
			name: "corner case - UUID с нулями",
			request: &proto.GetOrderStatusRequest{
				OrderId: "00000000-0000-0000-0000-000000000000",
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					uuid.Nil,
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusUnspecified, serviceErrors.ErrOrderNotFound)
			},
			expectedCode: codes.NotFound,
			checkResponse: func(t *testing.T, resp *proto.GetOrderStatusResponse) {
				assert.Nil(t, resp)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			mockOrder := new(mocks.Order)
			tt.setupMocks(mockOrder)

			server := &serverAPI{
				service: mockOrder,
			}

			ctx := context.Background()

			resp, err := server.GetOrderStatus(ctx, tt.request)

			if tt.expectedCode != codes.OK {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, tt.expectedCode, st.Code())
			} else {
				require.NoError(t, err)
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}

			mockOrder.AssertExpectations(t)
		})
	}
}
