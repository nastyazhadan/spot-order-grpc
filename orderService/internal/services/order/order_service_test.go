package order

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/mocks"
	storageErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"

	fakeValue "github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const CreateTimeout = 5 * time.Second

func TestCreateOrder(t *testing.T) {
	fakeValue.Seed(time.Now().UnixNano())

	randomUUID := uuid.New()
	randomPrice := sharedModels.NewDecimal(
		fmt.Sprintf("%.2f", fakeValue.Float64Range(1, 1000)))
	randomQuantity := int64(fakeValue.IntRange(1, 1000))

	tests := []struct {
		name           string
		userID         uuid.UUID
		marketID       uuid.UUID
		orderType      shared.OrderType
		price          sharedModels.Decimal
		quantity       int64
		setupMocks     func(*mocks.Saver, *mocks.MarketViewer, *mocks.RateLimiter, *mocks.RateLimiter)
		expectedStatus shared.OrderStatus
		expectedErr    error
		expectedErrMsg string
		checkResult    func(t *testing.T, orderID uuid.UUID, status shared.OrderStatus)
	}{
		{
			name:      "успешное создание заказа",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeTakeProfit,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.Saver, viewer *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				marketID := randomUUID
				markets := []sharedModels.Market{
					{
						ID:      marketID,
						Enabled: true,
					},
				}

				createLimiter.On("Allow", mock.Anything, randomUUID).Return(true, nil)
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).
					Return(nil)
			},
			expectedStatus: shared.OrderStatusCreated,
			expectedErr:    nil,
			checkResult: func(t *testing.T, orderID uuid.UUID, status shared.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
				assert.Equal(t, shared.OrderStatusCreated, status)
			},
		},
		{
			name:      "ошибка - превышен rate limit",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(_ *mocks.Saver, _ *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				createLimiter.On("Allow", mock.Anything, randomUUID).Return(false, nil)
			},
			expectedStatus: shared.OrderStatusCancelled,
			expectedErr:    serviceErrors.ErrRateLimitExceeded,
			checkResult: func(t *testing.T, orderID uuid.UUID, status shared.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, shared.OrderStatusCancelled, status)
			},
		},
		{
			name:      "ошибка - rate limiter недоступен",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(_ *mocks.Saver, _ *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				createLimiter.On("Allow", mock.Anything, randomUUID).Return(false, errors.New("cache down"))
			},
			expectedStatus: shared.OrderStatusCancelled,
			expectedErrMsg: "OrderService.CreateOrder: cache down",
		},
		{
			name:      "ошибка - рынок не найден",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(_ *mocks.Saver, viewer *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				createLimiter.On("Allow", mock.Anything, mock.Anything).Return(true, nil)
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return([]sharedModels.Market{}, nil)
			},
			expectedStatus: shared.OrderStatusCancelled,
			expectedErr:    serviceErrors.ErrMarketsNotFound,
		},
		{
			name:      "ошибка - заказ уже существует",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.Saver, viewer *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				marketID := randomUUID
				markets := []sharedModels.Market{{
					ID:      marketID,
					Enabled: true,
				}}

				createLimiter.On("Allow", mock.Anything, randomUUID).Return(true, nil)
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).Return(storageErrors.ErrOrderAlreadyExists)
			},
			expectedStatus: shared.OrderStatusCancelled,
			expectedErr:    serviceErrors.ErrOrderAlreadyExists,
			checkResult: func(t *testing.T, orderID uuid.UUID, status shared.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, shared.OrderStatusCancelled, status)
			},
		},
		{
			name:      "ошибка - недоступность сервиса рынков",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(_ *mocks.Saver, viewer *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				createLimiter.On("Allow", mock.Anything, randomUUID).Return(true, nil)
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(nil, errors.New("internal error"))

			},
			expectedStatus: shared.OrderStatusCancelled,
			expectedErrMsg: "OrderService.CreateOrder: internal error",
			checkResult: func(t *testing.T, orderID uuid.UUID, status shared.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, shared.OrderStatusCancelled, status)
			},
		},
		{
			name:      "ошибка - неизвестная ошибка при сохранении",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeMarket,
			price:     randomPrice,
			quantity:  randomQuantity,
			setupMocks: func(saver *mocks.Saver, viewer *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				marketID := randomUUID
				markets := []sharedModels.Market{{
					ID:      marketID,
					Enabled: true,
				}}

				createLimiter.On("Allow", mock.Anything, randomUUID).Return(true, nil)
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).
					Return(errors.New("internal error"))
			},
			expectedStatus: shared.OrderStatusCancelled,
			expectedErrMsg: "OrderService.CreateOrder: internal error",
			checkResult: func(t *testing.T, orderID uuid.UUID, status shared.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
				assert.Equal(t, shared.OrderStatusCancelled, status)
			},
		},
		{
			name:      "corner case - минимальное количество",
			userID:    randomUUID,
			marketID:  randomUUID,
			orderType: shared.OrderTypeTakeProfit,
			price:     randomPrice,
			quantity:  1,
			setupMocks: func(saver *mocks.Saver, viewer *mocks.MarketViewer, createLimiter *mocks.RateLimiter, _ *mocks.RateLimiter) {
				marketID := randomUUID
				markets := []sharedModels.Market{{
					ID:      marketID,
					Enabled: true,
				}}

				createLimiter.On("Allow", mock.Anything, randomUUID).Return(true, nil)
				viewer.On("ViewMarkets", mock.Anything, []sharedModels.UserRole{sharedModels.UserRoleUser}).
					Return(markets, nil)
				saver.On("SaveOrder", mock.Anything, mock.AnythingOfType("models.Order")).
					Return(nil)
			},
			expectedStatus: shared.OrderStatusCreated,
			expectedErr:    nil,
			checkResult: func(t *testing.T, orderID uuid.UUID, status shared.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
				assert.Equal(t, shared.OrderStatusCreated, status)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockSaver := mocks.NewSaver(t)
			mockGetter := mocks.NewGetter(t)
			mockMarketViewer := mocks.NewMarketViewer(t)
			mockCreateLimiter := mocks.NewRateLimiter(t)
			mockGetLimiter := mocks.NewRateLimiter(t)

			if test.setupMocks != nil {
				test.setupMocks(mockSaver, mockMarketViewer, mockCreateLimiter, mockGetLimiter)
			}

			service := New(mockSaver, mockGetter, mockMarketViewer, mockCreateLimiter, mockGetLimiter, CreateTimeout)
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

			switch test.name {
			case "ошибка - превышен rate limit", "ошибка - rate limiter недоступен":
				assertCreateShortCircuit(t, mockSaver, mockMarketViewer)
			}

			assert.Equal(t, test.expectedStatus, status)
			if test.checkResult != nil {
				test.checkResult(t, orderID, status)
			}

			mockSaver.AssertExpectations(t)
			mockMarketViewer.AssertExpectations(t)
			mockCreateLimiter.AssertExpectations(t)
			mockGetLimiter.AssertExpectations(t)
		})
	}
}

func TestGetOrderStatus(t *testing.T) {
	fakeValue.Seed(time.Now().UnixNano())

	userID := uuid.New()
	orderID := uuid.New()
	anotherUserID := uuid.New()

	randomUUID := uuid.New()
	randomPrice := sharedModels.NewDecimal(
		fmt.Sprintf("%.2f", fakeValue.Float64Range(1, 1000)))
	randomQuantity := int64(fakeValue.IntRange(1, 1000))

	tests := []struct {
		name                string
		orderID             uuid.UUID
		userID              uuid.UUID
		setupMocks          func(*mocks.Getter)
		getRateLimiterAllow bool
		getRateLimiterError error
		expectedStatus      shared.OrderStatus
		expectedError       error
		expectedErrMsg      string
	}{
		{
			name:    "успешное получение статуса заказа",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.Getter) {
				order := shared.Order{
					ID:        orderID,
					UserID:    userID,
					MarketID:  randomUUID,
					Type:      shared.OrderTypeMarket,
					Price:     randomPrice,
					Quantity:  randomQuantity,
					Status:    shared.OrderStatusCreated,
					CreatedAt: time.Now().UTC(),
				}
				getter.On("GetOrder", mock.Anything, orderID).
					Return(order, nil)
			},
			getRateLimiterAllow: true,
			expectedStatus:      shared.OrderStatusCreated,
			expectedError:       nil,
		},
		{
			name:                "ошибка - превышен rate limit",
			orderID:             orderID,
			userID:              userID,
			setupMocks:          nil,
			getRateLimiterAllow: false,
			expectedStatus:      shared.OrderStatusUnspecified,
			expectedError:       serviceErrors.ErrRateLimitExceeded,
		},
		{
			name:                "ошибка - rate limiter недоступен",
			orderID:             orderID,
			userID:              userID,
			setupMocks:          nil,
			getRateLimiterAllow: false,
			getRateLimiterError: errors.New("rate limiter error"),
			expectedStatus:      shared.OrderStatusUnspecified,
			expectedErrMsg:      "rate limiter error",
		},
		{
			name:    "ошибка - заказ не найден",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.Getter) {
				getter.On("GetOrder", mock.Anything, orderID).
					Return(shared.Order{}, storageErrors.ErrOrderNotFound)
			},
			getRateLimiterAllow: true,
			expectedStatus:      shared.OrderStatusUnspecified,
			expectedError:       serviceErrors.ErrOrderNotFound,
		},
		{
			name:    "ошибка - доступ запрещен (чужой заказ)",
			orderID: orderID,
			userID:  anotherUserID,
			setupMocks: func(getter *mocks.Getter) {
				order := shared.Order{
					ID:        orderID,
					UserID:    userID,
					MarketID:  randomUUID,
					Type:      shared.OrderTypeMarket,
					Price:     randomPrice,
					Quantity:  randomQuantity,
					Status:    shared.OrderStatusCreated,
					CreatedAt: time.Now().UTC(),
				}
				getter.On("GetOrder", mock.Anything, orderID).
					Return(order, nil)
			},
			getRateLimiterAllow: true,
			expectedStatus:      shared.OrderStatusUnspecified,
			expectedError:       serviceErrors.ErrOrderNotFound,
		},
		{
			name:    "ошибка - база данных недоступна",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.Getter) {
				getter.On("GetOrder", mock.Anything, orderID).
					Return(shared.Order{}, errors.New("internal error"))
			},
			getRateLimiterAllow: true,
			expectedStatus:      shared.OrderStatusUnspecified,
			expectedErrMsg:      "OrderService.GetOrderStatus: internal error",
		},
		{
			name:    "corner case - несуществующий UUID",
			orderID: uuid.Nil,
			userID:  userID,
			setupMocks: func(getter *mocks.Getter) {
				getter.On("GetOrder", mock.Anything, uuid.Nil).
					Return(shared.Order{}, storageErrors.ErrOrderNotFound)
			},
			getRateLimiterAllow: true,
			expectedStatus:      shared.OrderStatusUnspecified,
			expectedError:       serviceErrors.ErrOrderNotFound,
		},
		{
			name:    "проверка статуса - Cancelled",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(getter *mocks.Getter) {
				order := shared.Order{
					ID:        orderID,
					UserID:    userID,
					MarketID:  randomUUID,
					Type:      shared.OrderTypeStopLoss,
					Price:     randomPrice,
					Quantity:  randomQuantity,
					Status:    shared.OrderStatusCancelled,
					CreatedAt: time.Now().UTC(),
				}
				getter.On("GetOrder", mock.Anything, orderID).
					Return(order, nil)
			},
			getRateLimiterAllow: true,
			expectedStatus:      shared.OrderStatusCancelled,
			expectedError:       nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockSaver := mocks.NewSaver(t)
			mockGetter := mocks.NewGetter(t)
			mockMarketViewer := mocks.NewMarketViewer(t)
			mockCreateLimiter := mocks.NewRateLimiter(t)
			mockGetLimiter := mocks.NewRateLimiter(t)

			mockGetLimiter.On("Allow", mock.Anything, test.userID).
				Return(test.getRateLimiterAllow, test.getRateLimiterError)

			if test.setupMocks != nil {
				test.setupMocks(mockGetter)
			}

			service := New(
				mockSaver,
				mockGetter,
				mockMarketViewer,
				mockCreateLimiter,
				mockGetLimiter,
				CreateTimeout,
			)
			ctx := context.Background()

			status, err := service.GetOrderStatus(ctx, test.orderID, test.userID)

			if test.expectedError != nil || test.expectedErrMsg != "" {
				require.Error(t, err)

				if test.expectedError != nil {
					assert.ErrorIs(t, err, test.expectedError)
				}
				if test.expectedErrMsg != "" {
					assert.ErrorContains(t, err, test.expectedErrMsg)
				}
			} else {
				require.NoError(t, err)
			}

			switch test.name {
			case "ошибка - превышен rate limit", "ошибка - rate limiter недоступен":
				assertGetShortCircuit(t, mockGetter)
			}

			assert.Equal(t, test.expectedStatus, status)
			mockGetter.AssertExpectations(t)
			mockGetLimiter.AssertExpectations(t)
		})
	}
}

func assertCreateShortCircuit(
	test *testing.T,
	saver *mocks.Saver,
	viewer *mocks.MarketViewer,
) {
	test.Helper()
	viewer.AssertNotCalled(test, "ViewMarkets", mock.Anything, mock.Anything)
	saver.AssertNotCalled(test, "SaveOrder", mock.Anything, mock.Anything)
}

func assertGetShortCircuit(
	test *testing.T,
	getter *mocks.Getter,
) {
	test.Helper()
	getter.AssertNotCalled(test, "GetOrder", mock.Anything, mock.Anything)
}
