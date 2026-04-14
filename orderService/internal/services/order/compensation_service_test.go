package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/mocks"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
)

const (
	compensationTimeout = 5 * time.Second
	testServiceName     = "order-compensation-test"
	testTopic           = "market.state.changed"
	testGroup           = "order-service"
)

func testCompensationConfig() config.OrderConfig {
	return config.OrderConfig{
		Service:  config.ServiceConfig{Name: testServiceName},
		Timeouts: config.TimeoutsConfig{Service: compensationTimeout},
	}
}

type compensationDeps struct {
	manager    *mocks.TransactionManager
	inbox      *mocks.MarketInboxWriter
	canceler   *mocks.MarketOrderCanceler
	blockStore *mocks.MarketBlockStore
	producer   *mocks.OrderEventProducer
}

func newCompensationDeps(t *testing.T) *compensationDeps {
	return &compensationDeps{
		manager:    mocks.NewTransactionManager(t),
		inbox:      mocks.NewMarketInboxWriter(t),
		canceler:   mocks.NewMarketOrderCanceler(t),
		blockStore: &mocks.MarketBlockStore{},
		producer:   mocks.NewOrderEventProducer(t),
	}
}

func (d *compensationDeps) service() *CompensationService {
	return NewCompensationService(
		d.manager, d.inbox, d.canceler, d.blockStore, d.producer,
		zapLogger.NewNop(),
		testCompensationConfig(),
	)
}

func (d *compensationDeps) beginTx(commit error) *mockTx {
	tx := &mockTx{}
	tx.On("Commit", mock.Anything).Return(commit)
	d.manager.On("Begin", mock.Anything).Return(tx, nil)
	return tx
}

func (d *compensationDeps) beginTxWithRollback() *mockTx {
	tx := &mockTx{}
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)
	d.manager.On("Begin", mock.Anything).Return(tx, nil)
	return tx
}

func makeEvent(enabled bool, deleted bool) sharedModels.MarketStateChangedEvent {
	e := sharedModels.MarketStateChangedEvent{
		EventID:   uuid.New(),
		MarketID:  uuid.New(),
		Enabled:   enabled,
		UpdatedAt: time.Now().UTC(),
	}
	if deleted {
		now := time.Now().UTC()
		e.DeletedAt = &now
	}
	return e
}

func (d *compensationDeps) synchronizeBlockMaybe(marketID uuid.UUID, blocked bool) {
	d.blockStore.On("SynchronizeState", mock.Anything, marketID, blocked, mock.Anything).
		Return(true, nil).Maybe()
}

func TestProcessMarketStateChanged(t *testing.T) {
	payload := []byte(`{"market_id":"test"}`)

	tests := []struct {
		name           string
		event          sharedModels.MarketStateChangedEvent
		setupMocks     func(t *testing.T, d *compensationDeps)
		expectedErrMsg string
		checkErr       func(t *testing.T, err error)
	}{
		{
			name:  "enabled рынок — ордера не отменяются, commit, sync block",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				event := makeEvent(true, false)
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.inbox.On("MarkProcessed", mock.Anything, tx, event.EventID, testGroup).Return(nil).Maybe()
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
				d.synchronizeBlockMaybe(event.MarketID, false)
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "enabled рынок — commit вызывается, sync block called",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
				d.synchronizeBlockMaybe(uuid.Nil, false)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, false, mock.Anything).
					Return(true, nil).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "disabled рынок — ордера отменяются, events публикуются",
			event: makeEvent(false, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				cancelledIDs := []uuid.UUID{uuid.New(), uuid.New()}
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return(cancelledIDs, nil)
				d.producer.On("ProduceOrderStatusUpdated", mock.Anything, tx,
					mock.AnythingOfType("models.OrderStatusUpdatedEvent"),
				).Return(nil).Times(len(cancelledIDs))
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, true, mock.Anything).
					Return(true, nil).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "deleted рынок — ордера отменяются, events публикуются",
			event: makeEvent(true, true),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				cancelledIDs := []uuid.UUID{uuid.New()}
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return(cancelledIDs, nil)
				d.producer.On("ProduceOrderStatusUpdated", mock.Anything, tx,
					mock.AnythingOfType("models.OrderStatusUpdatedEvent"),
				).Return(nil).Once()
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, true, mock.Anything).
					Return(true, nil).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "disabled+deleted рынок — ордера отменяются",
			event: makeEvent(false, true),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return([]uuid.UUID{}, nil)
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, true, mock.Anything).
					Return(true, nil).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "нет активных ордеров для отмены — нет событий статуса",
			event: makeEvent(false, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return([]uuid.UUID{}, nil)
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, true, mock.Anything).
					Return(true, nil).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "событие уже обработано (InboxEventStatusProcessed) — skip, resync block",
			event: makeEvent(false, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(false, models.InboxEventStatusProcessed, nil)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, true, mock.Anything).
					Return(true, nil).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "событие в процессе (InboxEventStatusProcessing) — skip, resync block",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(false, models.InboxEventStatusProcessing, nil)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, false, mock.Anything).
					Return(true, nil).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:  "ошибка - Begin транзакции",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				d.manager.On("Begin", mock.Anything).Return((*mockTx)(nil), errors.New("pg pool exhausted"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "begin transaction")
				assert.ErrorContains(t, err, "pg pool exhausted")
			},
		},
		{
			name:  "ошибка - BeginProcessing — rollback, SaveFailed",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTxWithRollback()
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(false, models.InboxEventStatusFailed, errors.New("inbox write failed"))
				d.inbox.On("SaveFailed", mock.Anything,
					mock.AnythingOfType("models.InboxEvent"), mock.AnythingOfType("string"),
				).Return(nil)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "inbox write failed")
			},
		},
		{
			name:  "ошибка - BeginProcessing и SaveFailed тоже падает — обе ошибки в сообщении",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTxWithRollback()
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(false, models.InboxEventStatusFailed, errors.New("inbox error"))
				d.inbox.On("SaveFailed", mock.Anything,
					mock.AnythingOfType("models.InboxEvent"), mock.AnythingOfType("string"),
				).Return(errors.New("persist failed"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "inbox error")
				assert.ErrorContains(t, err, "persist failed")
			},
		},
		{
			name:  "ошибка - CancelActiveOrdersByMarket — rollback, SaveFailed",
			event: makeEvent(false, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTxWithRollback()
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return(nil, errors.New("cancel query failed"))
				d.inbox.On("SaveFailed", mock.Anything,
					mock.AnythingOfType("models.InboxEvent"), mock.AnythingOfType("string"),
				).Return(nil)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "cancel query failed")
			},
		},
		{
			name:  "ошибка - ProduceOrderStatusUpdated — rollback, SaveFailed",
			event: makeEvent(false, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTxWithRollback()
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return([]uuid.UUID{uuid.New()}, nil)
				d.producer.On("ProduceOrderStatusUpdated", mock.Anything, tx,
					mock.AnythingOfType("models.OrderStatusUpdatedEvent"),
				).Return(errors.New("kafka unavailable"))
				d.inbox.On("SaveFailed", mock.Anything,
					mock.AnythingOfType("models.InboxEvent"), mock.AnythingOfType("string"),
				).Return(nil)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "kafka unavailable")
			},
		},
		{
			name:  "ошибка - ProduceOrderStatusUpdated для второго ордера — первый уже записан",
			event: makeEvent(false, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTxWithRollback()
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				cancelledIDs := []uuid.UUID{uuid.New(), uuid.New()}
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return(cancelledIDs, nil)
				d.producer.On("ProduceOrderStatusUpdated", mock.Anything, tx,
					mock.AnythingOfType("models.OrderStatusUpdatedEvent"),
				).Return(nil).Once()
				d.producer.On("ProduceOrderStatusUpdated", mock.Anything, tx,
					mock.AnythingOfType("models.OrderStatusUpdatedEvent"),
				).Return(errors.New("outbox full")).Once()
				d.inbox.On("SaveFailed", mock.Anything,
					mock.AnythingOfType("models.InboxEvent"), mock.AnythingOfType("string"),
				).Return(nil)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "outbox full")
			},
		},
		{
			name:  "ошибка - MarkProcessed — rollback, SaveFailed",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTxWithRollback()
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).
					Return(errors.New("mark failed"))
				d.inbox.On("SaveFailed", mock.Anything,
					mock.AnythingOfType("models.InboxEvent"), mock.AnythingOfType("string"),
				).Return(nil)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "mark failed")
			},
		},
		{
			name:  "ошибка - commit транзакции",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := &mockTx{}
				tx.On("Commit", mock.Anything).Return(errors.New("commit failed"))
				tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)
				d.manager.On("Begin", mock.Anything).Return(tx, nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "commit transaction")
			},
		},
		{
			name:  "ошибка - commit транзакции skip-пути",
			event: makeEvent(true, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := &mockTx{}
				tx.On("Commit", mock.Anything).Return(errors.New("commit skipped failed"))
				tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)
				d.manager.On("Begin", mock.Anything).Return(tx, nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(false, models.InboxEventStatusProcessed, nil)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "commit skipped")
			},
		},
		{
			name:  "sync block падает — ошибка не пробрасывается (best effort)",
			event: makeEvent(false, false),
			setupMocks: func(t *testing.T, d *compensationDeps) {
				tx := d.beginTx(nil)
				d.inbox.On("BeginProcessing", mock.Anything, tx, mock.AnythingOfType("models.InboxEvent")).
					Return(true, models.InboxEventStatusProcessing, nil)
				d.canceler.On("CancelActiveOrdersByMarket", mock.Anything, tx, mock.Anything).
					Return([]uuid.UUID{}, nil)
				d.inbox.On("MarkProcessed", mock.Anything, tx, mock.Anything, testGroup).Return(nil)
				d.blockStore.On("SynchronizeState", mock.Anything, mock.Anything, true, mock.Anything).
					Return(false, errors.New("redis down")).Maybe()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newCompensationDeps(t)
			tt.setupMocks(t, d)

			svc := d.service()
			err := svc.ProcessMarketStateChanged(
				context.Background(),
				testTopic, testGroup,
				payload, tt.event,
			)

			tt.checkErr(t, err)

			d.blockStore.AssertExpectations(t)
		})
	}
}
