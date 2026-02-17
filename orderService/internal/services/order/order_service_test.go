package order

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	fakeValue "github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/mocks"
	storageErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
	"google.golang.org/genproto/googleapis/type/decimal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var randomUUID = uuid.New()

var randomPrice = models.Decimal(&decimal.Decimal{
	Value: fmt.Sprintf("%.2f", fakeValue.Float64Range(1, 1000)),
})

var randomQuantity = int64(fakeValue.IntRange(1, 1000))

func TestCreateOrder(t *testing.T) {
	fakeValue.Seed(time.Now().UnixNano())

	tests := []struct {
		name           string
		userID         uuid.UUID
		marketID       uuid.UUID
		orderType      models.OrderType
		price          models.Decimal
		quantity       int64
		setupMocks     func(*mocks.MockSaver, *mocks.MockOrderMarketViewer)
		expectedStatus models.OrderStatus
		expectedErr    error
		expectedErrMsg string
		checkResult    func(t *testing.T, orderID uuid.UUID, status models.OrderStatus)
	}{
		{
			name:      "успешное создание заказа",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: models.OrderTypeTakeProfit,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.MockSaver, viewer *mocks.MockOrderMarketViewer) {
				marketID := randomUUID
				markets := []sharedModels.Market{
					{
						ID:      marketID,
						Enabled: true,
					},
				}
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).
					Return(nil)
			},
			expectedStatus: models.OrderStatusCreated,
			expectedErr:    nil,
			checkResult: func(t *testing.T, orderID uuid.UUID, status models.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
				assert.Equal(t, models.OrderStatusCreated, status)
			},
		},
		{
			name:      "ошибка - рынок не найден",
			userID:    uuid.New(),
			marketID:  uuid.New(),
			orderType: models.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.MockSaver, viewer *mocks.MockOrderMarketViewer) {

				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return([]sharedModels.Market{}, nil)
			},
			expectedStatus: models.OrderStatusCancelled,
			expectedErr:    serviceErrors.ErrMarketsNotFound,
			checkResult: func(t *testing.T, orderID uuid.UUID, status models.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, models.OrderStatusCancelled, status)
			},
		},
		{
			name:      "ошибка - заказ уже существует",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: models.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.MockSaver, viewer *mocks.MockOrderMarketViewer) {
				marketID := randomUUID
				markets := []sharedModels.Market{
					{
						ID:      marketID,
						Enabled: true,
					},
				}
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).
					Return(storageErrors.ErrOrderAlreadyExists)
			},
			expectedStatus: models.OrderStatusCancelled,
			expectedErr:    serviceErrors.ErrOrderAlreadyExists,
			checkResult: func(t *testing.T, orderID uuid.UUID, status models.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, models.OrderStatusCancelled, status)
			},
		},
		{
			name:      "ошибка - недоступность сервиса рынков",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: models.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.MockSaver, viewer *mocks.MockOrderMarketViewer) {
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(nil, errors.New("internal error"))
			},
			expectedStatus: models.OrderStatusCancelled,
			expectedErrMsg: "Service.CreateOrder: internal error",
			checkResult: func(t *testing.T, orderID uuid.UUID, status models.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, models.OrderStatusCancelled, status)
			},
		},
		{
			name:      "ошибка - неизвестная ошибка при сохранении",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: models.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.MockSaver, viewer *mocks.MockOrderMarketViewer) {
				marketID := randomUUID
				markets := []sharedModels.Market{
					{
						ID:      marketID,
						Enabled: true,
					},
				}
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).
					Return(errors.New("internal error"))
			},
			expectedStatus: models.OrderStatusCancelled,
			expectedErrMsg: "Service.CreateOrder: internal error",
			checkResult: func(t *testing.T, orderID uuid.UUID, status models.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, models.OrderStatusCancelled, status)
			},
		},
		{
			name:      "corner case - минимальное количество",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: models.OrderTypeTakeProfit,
			price:     randomPrice,
			quantity:  1,
			setupMocks: func(saver *mocks.MockSaver, viewer *mocks.MockOrderMarketViewer) {
				marketID := randomUUID
				markets := []sharedModels.Market{
					{
						ID:      marketID,
						Enabled: true,
					},
				}
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).
					Return(nil)
			},
			expectedStatus: models.OrderStatusCreated,
			expectedErr:    nil,
			checkResult: func(t *testing.T, orderID uuid.UUID, status models.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
				assert.Equal(t, models.OrderStatusCreated, status)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockSaver := new(mocks.MockSaver)
			mockGetter := new(mocks.MockGetter)
			mockMarketViewer := new(mocks.MockOrderMarketViewer)

			test.setupMocks(mockSaver, mockMarketViewer)

			service := NewService(mockSaver, mockGetter, mockMarketViewer)
			ctx := context.Background()

			orderID, status, err := service.CreateOrder(
				ctx,
				test.userID,
				test.marketID,
				test.orderType,
				test.price,
				test.quantity,
			)

			if test.expectedErr != nil || test.expectedErrMsg != "" {
				require.Error(t, err)

				if test.expectedErr != nil {
					assert.ErrorIs(t, err, test.expectedErr)
				}
				if test.expectedErrMsg != "" {
					assert.ErrorContains(t, err, test.expectedErrMsg)
				}
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, test.expectedStatus, status)
			if test.checkResult != nil {
				test.checkResult(t, orderID, status)
			}

			mockSaver.AssertExpectations(t)
			mockMarketViewer.AssertExpectations(t)
		})
	}
}

func TestGetOrderStatus(t *testing.T) {
	fakeValue.Seed(time.Now().UnixNano())

	userID := uuid.New()
	orderID := uuid.New()
	anotherUserID := uuid.New()

	tests := []struct {
		name           string
		orderID        uuid.UUID
		userID         uuid.UUID
		setupMocks     func(*mocks.MockGetter)
		expectedStatus models.OrderStatus
		expectedErr    error
		expectedErrMsg string
	}{
		{
			name:    "успешное получение статуса заказа",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.MockGetter) {
				order := models.Order{
					ID:        orderID,
					UserID:    userID,
					MarketID:  randomUUID,
					Type:      models.OrderTypeMarket,
					Price:     randomPrice,
					Quantity:  randomQuantity,
					Status:    models.OrderStatusCreated,
					CreatedAt: time.Now().UTC(),
				}
				getter.On("GetOrder", mock.Anything, orderID).
					Return(order, nil)
			},
			expectedStatus: models.OrderStatusCreated,
			expectedErr:    nil,
		},
		{
			name:    "ошибка - заказ не найден",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.MockGetter) {
				getter.On("GetOrder", mock.Anything, orderID).
					Return(models.Order{}, storageErrors.ErrOrderNotFound)
			},
			expectedStatus: models.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrOrderNotFound,
		},
		{
			name:    "ошибка - доступ запрещен (чужой заказ)",
			orderID: orderID,
			userID:  anotherUserID,
			setupMocks: func(getter *mocks.MockGetter) {
				order := models.Order{
					ID:        orderID,
					UserID:    userID,
					MarketID:  randomUUID,
					Type:      models.OrderTypeMarket,
					Price:     randomPrice,
					Quantity:  randomQuantity,
					Status:    models.OrderStatusCreated,
					CreatedAt: time.Now().UTC(),
				}
				getter.On("GetOrder", mock.Anything, orderID).
					Return(order, nil)
			},
			expectedStatus: models.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrOrderNotFound,
		},
		{
			name:    "ошибка - база данных недоступна",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.MockGetter) {
				getter.On("GetOrder", mock.Anything, orderID).
					Return(models.Order{}, errors.New("internal error"))
			},
			expectedStatus: models.OrderStatusUnspecified,
			expectedErrMsg: "Service.GetOrderStatus: internal error",
		},
		{
			name:    "corner case - несуществующий UUID",
			orderID: uuid.Nil,
			userID:  userID,
			setupMocks: func(getter *mocks.MockGetter) {
				getter.On("GetOrder", mock.Anything, uuid.Nil).
					Return(models.Order{}, storageErrors.ErrOrderNotFound)
			},
			expectedStatus: models.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrOrderNotFound,
		},
		{
			name:    "проверка статуса - Cancelled",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.MockGetter) {
				order := models.Order{
					ID:        orderID,
					UserID:    userID,
					MarketID:  randomUUID,
					Type:      models.OrderTypeStopLoss,
					Price:     randomPrice,
					Quantity:  randomQuantity,
					Status:    models.OrderStatusCancelled,
					CreatedAt: time.Now().UTC(),
				}
				getter.On("GetOrder", mock.Anything, orderID).
					Return(order, nil)
			},
			expectedStatus: models.OrderStatusCancelled,
			expectedErr:    nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockSaver := new(mocks.MockSaver)
			mockGetter := new(mocks.MockGetter)
			mockMarketViewer := new(mocks.MockOrderMarketViewer)

			test.setupMocks(mockGetter)

			service := NewService(mockSaver, mockGetter, mockMarketViewer)
			ctx := context.Background()

			status, err := service.GetOrderStatus(ctx, test.orderID, test.userID)

			if test.expectedErr != nil || test.expectedErrMsg != "" {
				require.Error(t, err)

				if test.expectedErr != nil {
					assert.ErrorIs(t, err, test.expectedErr)
				}
				if test.expectedErrMsg != "" {
					assert.ErrorContains(t, err, test.expectedErrMsg)
				}
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, test.expectedStatus, status)

			mockGetter.AssertExpectations(t)
		})
	}
}
