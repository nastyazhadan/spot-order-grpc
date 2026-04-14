package order

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	orderModel "github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

func newIdemService(adapter *mockIdempotencyAdapter) *IdempotencyService {
	return NewIdempotencyService(adapter, zapLogger.NewNop())
}

func TestBuildRequestHash(t *testing.T) {
	svc := newIdemService(&mockIdempotencyAdapter{})

	marketID := uuid.New()
	price100, _ := orderModel.NewDecimal("100.00")
	price200, _ := orderModel.NewDecimal("200.00")

	tests := []struct {
		name      string
		marketID  uuid.UUID
		orderType orderModel.OrderType
		price     orderModel.Decimal
		quantity  int64
		wantSame  *struct {
			marketID  uuid.UUID
			orderType orderModel.OrderType
			price     orderModel.Decimal
			quantity  int64
		}
	}{
		{
			name:      "одинаковые аргументы дают одинаковый хэш",
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     price100,
			quantity:  10,
			wantSame: &struct {
				marketID  uuid.UUID
				orderType orderModel.OrderType
				price     orderModel.Decimal
				quantity  int64
			}{marketID, orderModel.OrderTypeLimit, price100, 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h1 := svc.buildRequestHash(tt.marketID, tt.orderType, tt.price, tt.quantity)
			assert.NotEmpty(t, h1, "hash не должен быть пустым")

			if tt.wantSame != nil {
				h2 := svc.buildRequestHash(tt.wantSame.marketID, tt.wantSame.orderType, tt.wantSame.price, tt.wantSame.quantity)
				assert.Equal(t, h1, h2, "одинаковые аргументы должны давать одинаковый хэш")
			}
		})
	}

	t.Run("разные аргументы дают разные хэши", func(t *testing.T) {
		h1 := svc.buildRequestHash(marketID, orderModel.OrderTypeLimit, price100, 10)
		h2 := svc.buildRequestHash(marketID, orderModel.OrderTypeMarket, price100, 10)
		h3 := svc.buildRequestHash(marketID, orderModel.OrderTypeLimit, price200, 10)
		h4 := svc.buildRequestHash(marketID, orderModel.OrderTypeLimit, price100, 20)
		h5 := svc.buildRequestHash(uuid.New(), orderModel.OrderTypeLimit, price100, 10)

		assert.NotEqual(t, h1, h2, "разный orderType → разный хэш")
		assert.NotEqual(t, h1, h3, "разная price → разный хэш")
		assert.NotEqual(t, h1, h4, "разный quantity → разный хэш")
		assert.NotEqual(t, h1, h5, "разный marketID → разный хэш")
	})

	t.Run("хэш имеет ожидаемый формат sha256 hex (64 символа)", func(t *testing.T) {
		price, _ := orderModel.NewDecimal("1.00")
		h := svc.buildRequestHash(uuid.New(), orderModel.OrderTypeLimit, price, 1)
		assert.Len(t, h, 64)
	})
}

func TestCheckIdempotencyResult(t *testing.T) {
	cachedOrderID := uuid.New()

	tests := []struct {
		name           string
		result         IdempotencyResult
		expectedID     uuid.UUID
		expectedStatus orderModel.OrderStatus
		expectedErr    error
		expectedErrMsg string
	}{
		{
			name: "IsCompleted — возвращает кэшированный orderID и статус",
			result: IdempotencyResult{
				IsCompleted: true,
				OrderID:     cachedOrderID,
				OrderStatus: "created",
			},
			expectedID:     cachedOrderID,
			expectedStatus: orderModel.OrderStatusCreated,
		},
		{
			name: "IsCompleted с filled статусом",
			result: IdempotencyResult{
				IsCompleted: true,
				OrderID:     cachedOrderID,
				OrderStatus: "filled",
			},
			expectedID:     cachedOrderID,
			expectedStatus: orderModel.OrderStatusFilled,
		},
		{
			name: "IsCompleted с cancelled статусом",
			result: IdempotencyResult{
				IsCompleted: true,
				OrderID:     cachedOrderID,
				OrderStatus: "cancelled",
			},
			expectedID:     cachedOrderID,
			expectedStatus: orderModel.OrderStatusCancelled,
		},
		{
			name: "IsCompleted с неизвестным статусом — OrderStatusUnspecified",
			result: IdempotencyResult{
				IsCompleted: true,
				OrderID:     cachedOrderID,
				OrderStatus: "whatever",
			},
			expectedID:     cachedOrderID,
			expectedStatus: orderModel.OrderStatusUnspecified,
		},
		{
			name:           "IsProcessing — ErrOrderProcessing",
			result:         IdempotencyResult{IsProcessing: true},
			expectedID:     uuid.Nil,
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrOrderProcessing,
		},
		{
			name:           "неизвестное состояние (оба false) — ошибка с описанием",
			result:         IdempotencyResult{},
			expectedID:     uuid.Nil,
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "unknown idempotency state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newIdemService(&mockIdempotencyAdapter{})
			id, status, err := svc.checkIdempotencyResult(context.Background(), tt.result)

			if tt.expectedErr != nil || tt.expectedErrMsg != "" {
				require.Error(t, err)
				if tt.expectedErr != nil {
					assert.ErrorIs(t, err, tt.expectedErr)
				}
				if tt.expectedErrMsg != "" {
					assert.ErrorContains(t, err, tt.expectedErrMsg)
				}
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedID, id)
			assert.Equal(t, tt.expectedStatus, status)
		})
	}
}

func TestAcquire(t *testing.T) {
	userID := uuid.New()
	hash := "testhash"

	tests := []struct {
		name         string
		setupMock    func(adapter *mockIdempotencyAdapter)
		wantResult   IdempotencyResult
		wantAcquired bool
		wantErr      bool
	}{
		{
			name: "успешный acquire — возвращает результат и acquired=true",
			setupMock: func(a *mockIdempotencyAdapter) {
				a.On("Acquire", mock.Anything, userID, hash).
					Return(IdempotencyResult{}, true, nil)
			},
			wantAcquired: true,
		},
		{
			name: "acquire вернул acquired=false (дубликат)",
			setupMock: func(a *mockIdempotencyAdapter) {
				a.On("Acquire", mock.Anything, userID, hash).
					Return(IdempotencyResult{IsCompleted: true, OrderID: uuid.New()}, false, nil)
			},
			wantAcquired: false,
		},
		{
			name: "ошибка адаптера — пробрасывается",
			setupMock: func(a *mockIdempotencyAdapter) {
				a.On("Acquire", mock.Anything, userID, hash).
					Return(IdempotencyResult{}, false, errors.New("redis timeout"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &mockIdempotencyAdapter{}
			tt.setupMock(adapter)
			svc := newIdemService(adapter)

			_, acquired, err := svc.acquire(context.Background(), userID, hash)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantAcquired, acquired)
			}

			adapter.AssertExpectations(t)
		})
	}
}

func TestCompleteIdempotencyChecking(t *testing.T) {
	userID := uuid.New()
	orderID := uuid.New()
	hash := "hash123"

	tests := []struct {
		name      string
		setupMock func(adapter *mockIdempotencyAdapter)
	}{
		{
			name: "успешный Complete — вызывается с правильными аргументами",
			setupMock: func(a *mockIdempotencyAdapter) {
				a.On("Complete", mock.Anything, userID, hash, orderID,
					orderModel.OrderStatusCreated.String(),
				).Return(nil).Once()
			},
		},
		{
			name: "ошибка Complete — не пробрасывается (fire-and-forget)",
			setupMock: func(a *mockIdempotencyAdapter) {
				a.On("Complete", mock.Anything, userID, hash, orderID,
					orderModel.OrderStatusCreated.String(),
				).Return(errors.New("redis down")).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &mockIdempotencyAdapter{}
			tt.setupMock(adapter)
			svc := newIdemService(adapter)

			svc.completeIdempotencyChecking(context.Background(), userID, orderID, hash, orderModel.OrderStatusCreated)

			adapter.AssertExpectations(t)
		})
	}
}

func TestFailCleanup(t *testing.T) {
	userID := uuid.New()
	hash := "hash456"

	tests := []struct {
		name      string
		acquired  bool
		setupMock func(adapter *mockIdempotencyAdapter)
		checkMock func(t *testing.T, adapter *mockIdempotencyAdapter)
	}{
		{
			name:      "acquired=false — FailCleanup не вызывается",
			acquired:  false,
			setupMock: func(_ *mockIdempotencyAdapter) {},
			checkMock: func(t *testing.T, a *mockIdempotencyAdapter) {
				a.AssertNotCalled(t, "FailCleanup", mock.Anything, mock.Anything, mock.Anything)
			},
		},
		{
			name:     "acquired=true — FailCleanup вызывается с правильными аргументами",
			acquired: true,
			setupMock: func(a *mockIdempotencyAdapter) {
				a.On("FailCleanup", mock.Anything, userID, hash).Return(nil).Once()
			},
			checkMock: func(t *testing.T, a *mockIdempotencyAdapter) {
				a.AssertExpectations(t)
			},
		},
		{
			name:     "acquired=true, FailCleanup падает — ошибка не пробрасывается",
			acquired: true,
			setupMock: func(a *mockIdempotencyAdapter) {
				a.On("FailCleanup", mock.Anything, userID, hash).
					Return(errors.New("redis down")).Once()
			},
			checkMock: func(t *testing.T, a *mockIdempotencyAdapter) {
				a.AssertExpectations(t)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &mockIdempotencyAdapter{}
			tt.setupMock(adapter)
			svc := newIdemService(adapter)

			svc.failCleanup(context.Background(), userID, hash, tt.acquired)

			tt.checkMock(t, adapter)
		})
	}
}
