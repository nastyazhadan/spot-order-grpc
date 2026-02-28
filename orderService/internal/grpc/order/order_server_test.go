package order

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/mocks"
	proto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"

	fakeValue "github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var randomUUID = uuid.New()

var randomPrice = models.Decimal(&decimal.Decimal{
	Value: fmt.Sprintf("%.2f", fakeValue.Float64Range(1, 1000)),
})

var randomQuantity = int64(fakeValue.IntRange(1, 1000))

func TestCreateOrder(t *testing.T) {
	fakeValue.Seed(0)

	validUserID := randomUUID.String()
	validMarketID := randomUUID.String()
	validOrderID := randomUUID

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
				Price:     randomPrice,
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
				Price:     randomPrice,
				Quantity:  randomQuantity,
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
				Price:     randomPrice,
				Quantity:  randomQuantity,
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
				UserId:    "17327382020i8994-42=2==",
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     randomPrice,
				Quantity:  randomQuantity,
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
				Price:     randomPrice,
				Quantity:  randomQuantity,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - price равна nil",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     nil,
				Quantity:  randomQuantity,
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
				Quantity:  randomQuantity,
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
				Quantity:  randomQuantity,
			},
			setupMocks:   func(mockOrder *mocks.Order) {},
			expectedCode: codes.InvalidArgument,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
			},
		},
		{
			name: "валидация - price равна 0",
			request: &proto.CreateOrderRequest{
				UserId:    validUserID,
				MarketId:  validMarketID,
				OrderType: proto.OrderType_TYPE_MARKET,
				Price:     &decimal.Decimal{Value: "0"},
				Quantity:  randomQuantity,
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
				Quantity:  randomQuantity,
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
				Price:     randomPrice,
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
				Price:     randomPrice,
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
				Price:     randomPrice,
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
				Price:     randomPrice,
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
				Price:     randomPrice,
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
				Price:     randomPrice,
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
				).Return(uuid.Nil, models.OrderStatusCancelled, errors.New("internal error"))
			},
			expectedCode: codes.Internal,
			checkResponse: func(t *testing.T, resp *proto.CreateOrderResponse) {
				assert.Nil(t, resp)
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			mockOrder := new(mocks.Order)
			test.setupMocks(mockOrder)

			server := &serverAPI{
				service: mockOrder,
			}

			ctx := context.Background()

			response, err := server.CreateOrder(ctx, test.request)

			if test.expectedCode != codes.OK {
				require.Error(t, err)
				stat, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, test.expectedCode, stat.Code())
			} else {
				require.NoError(t, err)
			}

			if test.checkResponse != nil {
				test.checkResponse(t, response)
			}

			mockOrder.AssertExpectations(t)
		})
	}
}

func TestGetOrderStatus(t *testing.T) {
	fakeValue.Seed(0)

	validUserID := randomUUID.String()
	validOrderID := randomUUID.String()

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
				OrderId: "invalid382932jeje23",
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
				UserId:  "invalidi29329eij2jed293",
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
				OrderId: uuid.New().String(),
				UserId:  validUserID,
			},
			setupMocks: func(mockOrder *mocks.Order) {
				mockOrder.On("GetOrderStatus",
					mock.Anything,
					mock.AnythingOfType("uuid.UUID"),
					mock.AnythingOfType("uuid.UUID"),
				).Return(models.OrderStatusCreated, serviceErrors.ErrOrderNotFound)
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
				).Return(models.OrderStatusUnspecified, errors.New("internal error"))
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
			name: "corner case - UUID с нулями",
			request: &proto.GetOrderStatusRequest{
				OrderId: "00000000-0000-0000-0000-000000000000",
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			mockOrder := new(mocks.Order)
			test.setupMocks(mockOrder)

			server := &serverAPI{
				service: mockOrder,
			}

			ctx := context.Background()

			response, err := server.GetOrderStatus(ctx, test.request)

			if test.expectedCode != codes.OK {
				require.Error(t, err)
				stat, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, test.expectedCode, stat.Code())
			} else {
				require.NoError(t, err)
			}

			if test.checkResponse != nil {
				test.checkResponse(t, response)
			}

			mockOrder.AssertExpectations(t)
		})
	}
}
