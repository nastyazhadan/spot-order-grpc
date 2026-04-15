package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	orderModel "github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/mocks"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type mockIdempotencyAdapter struct {
	mock.Mock
}

func (m *mockIdempotencyAdapter) Acquire(ctx context.Context, userID uuid.UUID, requestHash string) (IdempotencyResult, bool, error) {
	ret := m.Called(ctx, userID, requestHash)
	return ret.Get(0).(IdempotencyResult), ret.Bool(1), ret.Error(2)
}

func (m *mockIdempotencyAdapter) Complete(ctx context.Context, userID uuid.UUID, requestHash string, orderID uuid.UUID, orderStatus string) error {
	return m.Called(ctx, userID, requestHash, orderID, orderStatus).Error(0)
}

func (m *mockIdempotencyAdapter) FailCleanup(ctx context.Context, userID uuid.UUID, requestHash string) error {
	return m.Called(ctx, userID, requestHash).Error(0)
}

type mockTx struct {
	mock.Mock
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	panic("unexpected: Begin")
}

func (m *mockTx) Commit(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockTx) Rollback(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("unexpected: CopyFrom")
}

func (m *mockTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("unexpected: SendBatch")
}

func (m *mockTx) LargeObjects() pgx.LargeObjects {
	panic("unexpected: LargeObjects")
}

func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("unexpected: Prepare")
}

func (m *mockTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	panic("unexpected: Exec")
}

func (m *mockTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	panic("unexpected: Query")
}

func (m *mockTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	panic("unexpected: QueryRow")
}

func (m *mockTx) Conn() *pgx.Conn {
	panic("unexpected: Conn")
}

const (
	serviceTimeout = 5 * time.Second
	serviceName    = "order-test"
)

type deps struct {
	manager     *mocks.TransactionManager
	saver       *mocks.Saver
	getter      *mocks.Getter
	viewer      *mocks.MarketViewer
	blockStore  *mocks.MarketBlockStore
	createLim   *mocks.RateLimiter
	getLim      *mocks.RateLimiter
	producer    *mocks.EventProducer
	idemAdapter *mockIdempotencyAdapter
}

func newDeps(t *testing.T) *deps {
	return &deps{
		manager:     mocks.NewTransactionManager(t),
		saver:       mocks.NewSaver(t),
		getter:      mocks.NewGetter(t),
		viewer:      mocks.NewMarketViewer(t),
		blockStore:  &mocks.MarketBlockStore{},
		createLim:   mocks.NewRateLimiter(t),
		getLim:      mocks.NewRateLimiter(t),
		producer:    mocks.NewEventProducer(t),
		idemAdapter: &mockIdempotencyAdapter{},
	}
}

func (d *deps) service() *OrderService {
	cfg := config.OrderConfig{
		Redis: config.RedisConfig{
			Idempotency: config.IdempotencyConfig{
				CompleteAttempts:       testCompleteAttempts,
				CompleteAttemptTimeout: testCompleteAttemptTimeout,
				CompleteRetryDelay:     testCompleteRetryDelay,
				CleanupTimeout:         testCleanupTimeout,
			},
		},
	}

	idem := NewIdempotencyService(d.idemAdapter, zapLogger.NewNop(), cfg)
	return New(
		d.manager, d.saver, d.getter, d.viewer, d.blockStore,
		RateLimiters{Create: d.createLim, Get: d.getLim},
		Config{Timeout: serviceTimeout, ServiceName: serviceName},
		d.producer,
		*idem,
		zapLogger.NewNop(),
	)
}

func (d *deps) allowCreate(userID uuid.UUID) {
	d.createLim.On("Limit").Return(int64(100))
	d.createLim.On("Window").Return(time.Minute)
	d.createLim.On("Allow", mock.Anything, userID).Return(true, nil)
}

func (d *deps) denyCreate(userID uuid.UUID) {
	d.createLim.On("Limit").Return(int64(10))
	d.createLim.On("Window").Return(time.Second)
	d.createLim.On("Allow", mock.Anything, userID).Return(false, nil)
}

func (d *deps) errorCreate(userID uuid.UUID, err error) {
	d.createLim.On("Limit").Return(int64(10))
	d.createLim.On("Window").Return(time.Second)
	d.createLim.On("Allow", mock.Anything, userID).Return(false, err)
}

func (d *deps) allowGet(userID uuid.UUID) {
	d.getLim.On("Limit").Return(int64(100))
	d.getLim.On("Window").Return(time.Minute)
	d.getLim.On("Allow", mock.Anything, userID).Return(true, nil)
}

func (d *deps) denyGet(userID uuid.UUID) {
	d.getLim.On("Limit").Return(int64(10))
	d.getLim.On("Window").Return(time.Second)
	d.getLim.On("Allow", mock.Anything, userID).Return(false, nil)
}

func (d *deps) errorGet(userID uuid.UUID, err error) {
	d.getLim.On("Limit").Return(int64(10))
	d.getLim.On("Window").Return(time.Second)
	d.getLim.On("Allow", mock.Anything, userID).Return(false, err)
}

func (d *deps) idemAcquired(userID uuid.UUID) {
	d.idemAdapter.On("Acquire", mock.Anything, userID, mock.Anything).
		Return(IdempotencyResult{}, true, nil)
}

func (d *deps) idemComplete() {
	d.idemAdapter.On("Complete",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(nil)
}

func (d *deps) idemFailCleanup() {
	d.idemAdapter.On("FailCleanup", mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
}

func (d *deps) allowMarket(marketID uuid.UUID) {
	d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(false, nil)
	d.viewer.On("GetMarketByID", mock.Anything, marketID).
		Return(sharedModels.Market{ID: marketID, Enabled: true}, nil)
}

func (d *deps) beginTx(commit error) *mockTx {
	tx := &mockTx{}
	tx.On("Commit", mock.Anything).Return(commit)
	d.manager.On("Begin", mock.Anything).Return(tx, nil)
	return tx
}

func (d *deps) beginTxWithRollback() *mockTx {
	tx := &mockTx{}
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)
	d.manager.On("Begin", mock.Anything).Return(tx, nil)
	return tx
}

func mustDecimal(t *testing.T, raw string) orderModel.Decimal {
	t.Helper()
	d, err := orderModel.NewDecimal(raw)
	require.NoError(t, err, "failed to create test decimal %q", raw)
	return d
}

func assertCreateShortCircuit(t *testing.T, d *deps) {
	t.Helper()
	d.viewer.AssertNotCalled(t, "GetMarketByID", mock.Anything, mock.Anything)
	d.saver.AssertNotCalled(t, "SaveOrder", mock.Anything, mock.Anything, mock.Anything)
	d.manager.AssertNotCalled(t, "Begin", mock.Anything)
}

func assertGetShortCircuit(t *testing.T, d *deps) {
	t.Helper()
	d.getter.AssertNotCalled(t, "GetOrder", mock.Anything, mock.Anything, mock.Anything)
}

func TestCreateOrder(t *testing.T) {
	userID := uuid.New()
	marketID := uuid.New()

	tests := []struct {
		name           string
		userID         uuid.UUID
		marketID       uuid.UUID
		orderType      orderModel.OrderType
		price          string
		quantity       int64
		setupMocks     func(t *testing.T, d *deps)
		expectedStatus orderModel.OrderStatus
		expectedErr    error
		expectedErrMsg string
		checkResult    func(t *testing.T, orderID uuid.UUID, status orderModel.OrderStatus)
		shortCircuit   func(t *testing.T, d *deps)
	}{
		{
			name:      "успешное создание ордера",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.50",
			quantity:  10,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := d.beginTx(nil)
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).Return(nil)
				d.idemComplete()
			},
			expectedStatus: orderModel.OrderStatusCreated,
			checkResult: func(t *testing.T, orderID uuid.UUID, status orderModel.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
				assert.Equal(t, orderModel.OrderStatusCreated, status)
			},
		},
		{
			name:      "успешное создание - тип OrderTypeMarket",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeMarket,
			price:     "200.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := d.beginTx(nil)
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).Return(nil)
				d.idemComplete()
			},
			expectedStatus: orderModel.OrderStatusCreated,
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "успешное создание - минимальное количество",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeTakeProfit,
			price:     "0.00000001",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := d.beginTx(nil)
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).Return(nil)
				d.idemComplete()
			},
			expectedStatus: orderModel.OrderStatusCreated,
			checkResult: func(t *testing.T, orderID uuid.UUID, status orderModel.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
				assert.Equal(t, orderModel.OrderStatusCreated, status)
			},
		},
		{
			name:      "Order содержит переданные userID, marketID, тип и количество",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeStopLoss,
			price:     "50.00",
			quantity:  3,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := d.beginTx(nil)
				d.saver.On("SaveOrder", mock.Anything, tx,
					mock.MatchedBy(func(o models.Order) bool {
						return o.UserID == userID &&
							o.MarketID == marketID &&
							o.Type == orderModel.OrderTypeStopLoss &&
							o.Status == orderModel.OrderStatusCreated &&
							o.Quantity == 3
					}),
				).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).Return(nil)
				d.idemComplete()
			},
			expectedStatus: orderModel.OrderStatusCreated,
		},
		{
			name:      "идемпотентность - повторный запрос возвращает кэшированный ордер",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				cachedOrderID := uuid.New()
				d.idemAdapter.On("Acquire", mock.Anything, userID, mock.Anything).
					Return(IdempotencyResult{
						IsCompleted: true,
						OrderID:     cachedOrderID,
						OrderStatus: orderModel.OrderStatusCreated.String(),
					}, false, nil)
			},
			expectedStatus: orderModel.OrderStatusCreated,
			checkResult: func(t *testing.T, orderID uuid.UUID, status orderModel.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID, "должен вернуть кэшированный ID")
				assert.Equal(t, orderModel.OrderStatusCreated, status)
			},
			shortCircuit: func(t *testing.T, d *deps) { assertCreateShortCircuit(t, d) },
		},
		{
			name:      "идемпотентность - запрос обрабатывается — ErrOrderProcessing",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAdapter.On("Acquire", mock.Anything, userID, mock.Anything).
					Return(IdempotencyResult{IsProcessing: true}, false, nil)
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrOrderProcessing,
			shortCircuit: func(t *testing.T, d *deps) {
				assertCreateShortCircuit(t, d)
			},
		},
		{
			name:      "идемпотентность - ошибка acquire — fail fast",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  10,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAdapter.On("Acquire", mock.Anything, userID, mock.Anything).
					Return(IdempotencyResult{}, false, errors.New("redis timeout"))
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "redis timeout",
			shortCircuit: func(t *testing.T, d *deps) {
				assertCreateShortCircuit(t, d)
			},
		},
		{
			name:      "ошибка - rate limit создания превышен",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeMarket,
			price:     "50.00",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.denyCreate(userID)
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrRateLimitExceeded,
			shortCircuit: func(t *testing.T, d *deps) {
				assertCreateShortCircuit(t, d)
			},
		},
		{
			name:      "ошибка - rate limiter создания недоступен",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeMarket,
			price:     "50.00",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.errorCreate(userID, errors.New("cache down"))
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "cache down",
			shortCircuit: func(t *testing.T, d *deps) {
				assertCreateShortCircuit(t, d)
			},
		},
		{
			name:      "blockStore недоступен (не ctx ошибка) — fallback к GetMarketByID",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  2,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(false, errors.New("redis unreachable"))
				d.viewer.On("GetMarketByID", mock.Anything, marketID).
					Return(sharedModels.Market{ID: marketID, Enabled: true}, nil)
				tx := d.beginTx(nil)
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).Return(nil)
				d.idemComplete()
			},
			expectedStatus: orderModel.OrderStatusCreated,
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "blockStore возвращает ctx ошибку — fail fast без вызова GetMarketByID",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  2,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(false, context.DeadlineExceeded)
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "deadline exceeded",
		},
		{
			name:      "ошибка - рынок удалён (DeletedAt != nil)",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				deletedAt := time.Now().UTC()
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(false, nil)
				d.viewer.On("GetMarketByID", mock.Anything, marketID).
					Return(sharedModels.Market{ID: marketID, Enabled: true, DeletedAt: &deletedAt}, nil)
				d.blockStore.On("SynchronizeState", mock.Anything, marketID, true, mock.Anything).
					Return(true, nil).Maybe()
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    sharedErrors.ErrMarketNotFound{},
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "ошибка - рынок отключён (!market.Enabled)",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(false, nil)
				d.viewer.On("GetMarketByID", mock.Anything, marketID).
					Return(sharedModels.Market{ID: marketID, Enabled: false}, nil)
				d.blockStore.On("SynchronizeState", mock.Anything, marketID, true, mock.Anything).
					Return(true, nil).Maybe()
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrDisabled{},
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "ошибка - рынок временно недоступен (ErrMarketUnavailable)",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(false, nil)
				d.viewer.On("GetMarketByID", mock.Anything, marketID).
					Return(sharedModels.Market{}, serviceErrors.ErrMarketUnavailable)
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrMarketUnavailable,
		},
		{
			name:      "ошибка - GetMarketByID неизвестная ошибка",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(false, nil)
				d.viewer.On("GetMarketByID", mock.Anything, marketID).
					Return(sharedModels.Market{}, errors.New("spot service unavailable"))
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "spot service unavailable",
		},
		{
			name:      "рынок заблокирован в кэше, но GetMarketByID показывает enabled — снятие блокировки",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  2,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(true, nil)
				d.viewer.On("GetMarketByID", mock.Anything, marketID).
					Return(sharedModels.Market{ID: marketID, Enabled: true}, nil)
				d.blockStore.On("SynchronizeState", mock.Anything, marketID, false, mock.Anything).
					Return(true, nil).Maybe()
				tx := d.beginTx(nil)
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).Return(nil)
				d.idemComplete()
			},
			expectedStatus: orderModel.OrderStatusCreated,
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.NotEqual(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "рынок заблокирован, GetMarketByID возвращает ErrMarketUnavailable — fail closed",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  1,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.blockStore.On("IsBlocked", mock.Anything, marketID).Return(true, nil)
				d.viewer.On("GetMarketByID", mock.Anything, marketID).
					Return(sharedModels.Market{}, serviceErrors.ErrMarketUnavailable)
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrMarketUnavailable,
		},
		{
			name:      "ошибка - начало транзакции",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				d.manager.On("Begin", mock.Anything).Return((*mockTx)(nil), errors.New("pg connection error"))
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "begin transaction",
		},
		{
			name:      "ошибка - ордер уже существует (ErrOrderAlreadyExists → ErrAlreadyExists)",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := d.beginTxWithRollback()
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).
					Return(repositoryErrors.ErrOrderAlreadyExists)
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    sharedErrors.ErrAlreadyExists{},
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "ошибка - неизвестная ошибка при сохранении",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeMarket,
			price:     "100.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := d.beginTxWithRollback()
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).
					Return(errors.New("db write error"))
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "db write error",
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "ошибка - запись события в outbox (ProduceOrderCreated)",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := d.beginTxWithRollback()
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).
					Return(errors.New("outbox write failed"))
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "outbox write failed",
			checkResult: func(t *testing.T, orderID uuid.UUID, _ orderModel.OrderStatus) {
				assert.Equal(t, uuid.Nil, orderID)
			},
		},
		{
			name:      "ошибка - коммит транзакции",
			userID:    userID,
			marketID:  marketID,
			orderType: orderModel.OrderTypeLimit,
			price:     "100.00",
			quantity:  5,
			setupMocks: func(t *testing.T, d *deps) {
				d.idemAcquired(userID)
				d.allowCreate(userID)
				d.allowMarket(marketID)
				tx := &mockTx{}
				tx.On("Commit", mock.Anything).Return(errors.New("commit failed"))
				tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)
				d.manager.On("Begin", mock.Anything).Return(tx, nil)
				d.saver.On("SaveOrder", mock.Anything, tx, mock.AnythingOfType("models.Order")).Return(nil)
				d.producer.On("ProduceOrderCreated", mock.Anything, tx, mock.AnythingOfType("models.OrderCreatedEvent")).Return(nil)
				d.idemFailCleanup()
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "commit transaction",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeps(t)
			price := mustDecimal(t, tt.price)
			tt.setupMocks(t, d)

			svc := d.service()
			orderID, status, err := svc.CreateOrder(
				context.Background(),
				tt.userID, tt.marketID,
				tt.orderType, price, tt.quantity,
			)

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

			assert.Equal(t, tt.expectedStatus, status)

			if tt.checkResult != nil {
				tt.checkResult(t, orderID, status)
			}
			if tt.shortCircuit != nil {
				tt.shortCircuit(t, d)
			}

			d.blockStore.AssertExpectations(t)
			d.idemAdapter.AssertExpectations(t)
		})
	}
}

func TestGetOrderStatus(t *testing.T) {
	userID := uuid.New()
	orderID := uuid.New()

	baseOrder := func(status orderModel.OrderStatus) models.Order {
		return models.Order{
			ID:        orderID,
			UserID:    userID,
			MarketID:  uuid.New(),
			Type:      orderModel.OrderTypeLimit,
			Quantity:  10,
			Status:    status,
			CreatedAt: time.Now().UTC(),
		}
	}

	tests := []struct {
		name           string
		orderID        uuid.UUID
		userID         uuid.UUID
		setupMocks     func(t *testing.T, d *deps)
		expectedStatus orderModel.OrderStatus
		expectedErr    error
		expectedErrMsg string
		shortCircuit   func(t *testing.T, d *deps)
	}{
		{
			name:    "успешное получение статуса — StatusCreated",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.allowGet(userID)
				d.getter.On("GetOrder", mock.Anything, orderID, userID).
					Return(baseOrder(orderModel.OrderStatusCreated), nil)
			},
			expectedStatus: orderModel.OrderStatusCreated,
		},
		{
			name:    "успешное получение статуса — StatusPending",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.allowGet(userID)
				d.getter.On("GetOrder", mock.Anything, orderID, userID).
					Return(baseOrder(orderModel.OrderStatusPending), nil)
			},
			expectedStatus: orderModel.OrderStatusPending,
		},
		{
			name:    "GetOrder передаёт оба ID в репозиторий без изменений",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.allowGet(userID)
				d.getter.On("GetOrder", mock.Anything, orderID, userID).
					Return(baseOrder(orderModel.OrderStatusCreated), nil)
			},
			expectedStatus: orderModel.OrderStatusCreated,
		},
		{
			name:    "ошибка - rate limit получения превышен",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.denyGet(userID)
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    serviceErrors.ErrRateLimitExceeded,
			shortCircuit: func(t *testing.T, d *deps) {
				assertGetShortCircuit(t, d)
			},
		},
		{
			name:    "ошибка - rate limiter получения недоступен",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.errorGet(userID, errors.New("redis down"))
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "redis down",
			shortCircuit: func(t *testing.T, d *deps) {
				assertGetShortCircuit(t, d)
			},
		},
		{
			name:    "ошибка - ордер не найден (ErrOrderNotFound → ErrNotFound)",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.allowGet(userID)
				d.getter.On("GetOrder", mock.Anything, orderID, userID).
					Return(models.Order{}, repositoryErrors.ErrOrderNotFound)
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    sharedErrors.ErrNotFound{},
		},
		{
			name:    "ErrNotFound содержит ID несуществующего ордера",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.allowGet(userID)
				d.getter.On("GetOrder", mock.Anything, orderID, userID).
					Return(models.Order{}, repositoryErrors.ErrOrderNotFound)
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    sharedErrors.ErrNotFound{ID: orderID},
		},
		{
			name:    "ошибка - база данных недоступна",
			orderID: orderID,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.allowGet(userID)
				d.getter.On("GetOrder", mock.Anything, orderID, userID).
					Return(models.Order{}, errors.New("db connection lost"))
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErrMsg: "OrderService.GetOrderStatus: db connection lost",
		},
		{
			name:    "corner case - uuid.Nil в качестве orderID",
			orderID: uuid.Nil,
			userID:  userID,
			setupMocks: func(t *testing.T, d *deps) {
				d.allowGet(userID)
				d.getter.On("GetOrder", mock.Anything, uuid.Nil, userID).
					Return(models.Order{}, repositoryErrors.ErrOrderNotFound)
			},
			expectedStatus: orderModel.OrderStatusUnspecified,
			expectedErr:    sharedErrors.ErrNotFound{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeps(t)
			tt.setupMocks(t, d)

			svc := d.service()
			status, err := svc.GetOrderStatus(context.Background(), tt.orderID, tt.userID)

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

			assert.Equal(t, tt.expectedStatus, status)

			if tt.shortCircuit != nil {
				tt.shortCircuit(t, d)
			}

			d.blockStore.AssertExpectations(t)
			d.idemAdapter.AssertExpectations(t)
		})
	}
}
