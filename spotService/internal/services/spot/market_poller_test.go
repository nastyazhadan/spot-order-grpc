package spot

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

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	sharedModels "github.com/nastyazhadan/spot-order-grpc/shared/models"
	domainModels "github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/mocks"
)

const (
	testPollInterval      = 10 * time.Millisecond
	testProcessingTimeout = 2 * time.Second
	testBatchSize         = 3
)

func newTestPoller(
	reader *mocks.MarketReader,
	producer *mocks.MarketEventProducer,
	cursor *mocks.CursorStore,
	refresher *mocks.MarketCacheRefresher,
) *MarketPoller {
	var r MarketReader
	if reader != nil {
		r = reader
	}

	var p MarketEventProducer
	if producer != nil {
		p = producer
	}

	var c CursorStore
	if cursor != nil {
		c = cursor
	}

	var cr MarketCacheRefresher
	if refresher != nil {
		cr = refresher
	}

	return NewMarketPoller(
		r,
		p,
		c,
		cr,
		testPollInterval,
		testProcessingTimeout,
		testBatchSize,
		zapLogger.NewNop(),
	)
}

func makeMarket(enabled bool, deletedAt *time.Time) sharedModels.Market {
	return sharedModels.Market{
		ID:        uuid.New(),
		Name:      "test-market",
		Enabled:   enabled,
		DeletedAt: deletedAt,
		UpdatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func makeMarketsForMarketPoller(n int) []sharedModels.Market {
	markets := make([]sharedModels.Market, n)
	for i := range markets {
		markets[i] = makeMarket(true, nil)
	}
	return markets
}

func TestInit(t *testing.T) {
	fixedAt := time.Now().UTC().Truncate(time.Millisecond)
	fixedID := uuid.New()

	tests := []struct {
		name           string
		ctx            context.Context
		setupMocks     func(store *mocks.CursorStore)
		wantErr        bool
		checkErr       func(t *testing.T, err error)
		wantLastSeenAt time.Time
		wantLastSeenID uuid.UUID
	}{
		{
			name:       "nil ctx — ошибка без паники",
			ctx:        nil,
			setupMocks: func(_ *mocks.CursorStore) {},
			wantErr:    true,
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "nil context")
			},
		},
		{
			name: "pgx.ErrNoRows — курсор не найден, нулевые значения",
			ctx:  context.Background(),
			setupMocks: func(store *mocks.CursorStore) {
				store.On("Get", mock.Anything, marketStateChangedPollerName).
					Return(domainModels.PollerCursor{}, pgx.ErrNoRows)
			},
			wantLastSeenAt: time.Time{},
			wantLastSeenID: uuid.Nil,
		},
		{
			name: "курсор найден — lastSeenAt и lastSeenID выставляются корректно",
			ctx:  context.Background(),
			setupMocks: func(store *mocks.CursorStore) {
				store.On("Get", mock.Anything, marketStateChangedPollerName).
					Return(domainModels.PollerCursor{
						PollerName: marketStateChangedPollerName,
						LastSeenAt: fixedAt,
						LastSeenID: fixedID,
					}, nil)
			},
			wantLastSeenAt: fixedAt,
			wantLastSeenID: fixedID,
		},
		{
			name: "неизвестная ошибка хранилища — возвращается ошибка",
			ctx:  context.Background(),
			setupMocks: func(store *mocks.CursorStore) {
				store.On("Get", mock.Anything, marketStateChangedPollerName).
					Return(domainModels.PollerCursor{}, errors.New("pg: connection refused"))
			},
			wantErr: true,
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "pg: connection refused")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mocks.CursorStore{}
			reader := &mocks.MarketReader{}
			producer := &mocks.MarketEventProducer{}
			refresher := &mocks.MarketCacheRefresher{}
			tt.setupMocks(store)

			p := newTestPoller(reader, producer, store, refresher)
			err := p.Init(tt.ctx)

			if tt.checkErr != nil {
				tt.checkErr(t, err)
			} else if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantLastSeenAt, p.lastSeenAt)
				assert.Equal(t, tt.wantLastSeenID, p.lastSeenID)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestProcessNextBatch(t *testing.T) {
	markets := makeMarketsForMarketPoller(2)

	tests := []struct {
		name           string
		initialAt      time.Time
		initialID      uuid.UUID
		setupMocks     func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer)
		wantUpdatedIDs int
		wantHasMore    bool
		wantErr        bool
		checkErr       func(t *testing.T, err error)
		checkCursor    func(t *testing.T, p *MarketPoller)
		checkEvents    func(t *testing.T, producer *mocks.MarketEventProducer)
	}{
		{
			name: "пустой результат — nil, false, nil, курсор не меняется",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer) {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, nil)
			},
			wantUpdatedIDs: 0,
			wantHasMore:    false,
		},
		{
			name: "меньше batchSize маркетов — hasMore=false",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer) {
				markets := makeMarketsForMarketPoller(2)
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(markets, nil)
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
			wantUpdatedIDs: 2,
			wantHasMore:    false,
		},
		{
			name: "ровно batchSize маркетов — hasMore=true",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer) {
				markets := makeMarketsForMarketPoller(testBatchSize)
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(markets, nil)
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
			wantUpdatedIDs: testBatchSize,
			wantHasMore:    true,
		},
		{
			name: "курсор обновляется до последнего маркета в батче",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer) {
				last := markets[len(markets)-1]

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(markets, nil)
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(nil)

				_ = last
			},
			wantUpdatedIDs: 2,
			wantHasMore:    false,
			checkCursor: func(t *testing.T, p *MarketPoller) {
				last := markets[len(markets)-1]

				require.Equal(t, last.UpdatedAt.UTC(), p.lastSeenAt)
				require.Equal(t, last.ID, p.lastSeenID)
			},
		},
		{
			name:      "reader передаёт в ListUpdatedSince начальный курсор поллера",
			initialAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			initialID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer) {
				expectedAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				expectedID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
				reader.On("ListUpdatedSince", mock.Anything, expectedAt, expectedID, testBatchSize).
					Return(nil, nil)
			},
			wantHasMore: false,
		},
		{
			name:      "ошибка reader — возвращается ошибка, курсор не меняется",
			initialID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer) {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, errors.New("db timeout"))
			},
			wantErr: true,
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "db timeout")
			},
			checkCursor: func(t *testing.T, p *MarketPoller) {
				assert.Equal(t, uuid.MustParse("22222222-2222-2222-2222-222222222222"), p.lastSeenID)
			},
		},
		{
			name:      "ошибка producer — возвращается ошибка, курсор не меняется",
			initialID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer) {
				markets := makeMarketsForMarketPoller(1)
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(markets, nil)
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("kafka unavailable"))
			},
			wantErr: true,
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "kafka unavailable")
			},
			checkCursor: func(t *testing.T, p *MarketPoller) {
				assert.Equal(t, uuid.MustParse("33333333-3333-3333-3333-333333333333"), p.lastSeenID)
			},
		},
		{
			name: "события собраны с корректными полями (ID, Enabled, DeletedAt, UpdatedAt)",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer) {
				deletedAt := time.Now().UTC()
				market := sharedModels.Market{
					ID:        uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
					Enabled:   false,
					DeletedAt: &deletedAt,
					UpdatedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
				}

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return([]sharedModels.Market{market}, nil)

				producer.On("PublishMarketStateChanged", mock.Anything,
					mock.MatchedBy(func(events []sharedModels.MarketStateChangedEvent) bool {
						if len(events) != 1 {
							return false
						}
						e := events[0]
						return e.MarketID == market.ID &&
							e.Enabled == market.Enabled &&
							e.DeletedAt == market.DeletedAt &&
							e.UpdatedAt.Equal(market.UpdatedAt.UTC()) &&
							e.EventID != uuid.Nil
					}),
					mock.Anything,
				).Return(nil)
			},
			wantUpdatedIDs: 1,
			wantHasMore:    false,
		},
		{
			name: "updatedIDs содержат ID всех маркетов из батча",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer) {
				markets := makeMarketsForMarketPoller(2)
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(markets, nil)
				producer.On("PublishMarketStateChanged", mock.Anything,
					mock.MatchedBy(func(events []sharedModels.MarketStateChangedEvent) bool {
						if len(events) != 2 {
							return false
						}
						return events[0].MarketID == markets[0].ID &&
							events[1].MarketID == markets[1].ID
					}),
					mock.Anything,
				).Return(nil)
			},
			wantUpdatedIDs: 2,
			wantHasMore:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mocks.MarketReader{}
			producer := &mocks.MarketEventProducer{}
			cursor := &mocks.CursorStore{}
			refresher := &mocks.MarketCacheRefresher{}
			tt.setupMocks(reader, producer)

			p := newTestPoller(reader, producer, cursor, refresher)
			if tt.initialID != uuid.Nil {
				p.lastSeenID = tt.initialID
			}
			if !tt.initialAt.IsZero() {
				p.lastSeenAt = tt.initialAt
			}

			updatedIDs, hasMore, err := p.processNextBatch(context.Background())

			if tt.checkErr != nil {
				tt.checkErr(t, err)
			} else if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantHasMore, hasMore)
				assert.Len(t, updatedIDs, tt.wantUpdatedIDs)
			}

			if tt.checkCursor != nil {
				tt.checkCursor(t, p)
			}

			reader.AssertExpectations(t)
			producer.AssertExpectations(t)
		})
	}
}

func TestBuildNextPollerCursor(t *testing.T) {
	tests := []struct {
		name           string
		markets        []sharedModels.Market
		wantPollerName string
		wantLastSeenID uuid.UUID
		wantLastSeenAt time.Time
	}{
		{
			name: "один маркет — курсор берётся из него",
			markets: []sharedModels.Market{
				{
					ID:        uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
					UpdatedAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
				},
			},
			wantPollerName: marketStateChangedPollerName,
			wantLastSeenID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			wantLastSeenAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		{
			name: "несколько маркетов — курсор берётся из последнего",
			markets: []sharedModels.Market{
				{ID: uuid.New(), UpdatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
				{ID: uuid.New(), UpdatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
				{
					ID:        uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
					UpdatedAt: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
				},
			},
			wantPollerName: marketStateChangedPollerName,
			wantLastSeenID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			wantLastSeenAt: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "UpdatedAt в не-UTC таймзоне — конвертируется в UTC",
			markets: []sharedModels.Market{
				{
					ID:        uuid.New(),
					UpdatedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600)),
				},
			},
			wantPollerName: marketStateChangedPollerName,
			wantLastSeenAt: time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPoller(nil, nil, nil, nil)
			cursor := p.buildNextPollerCursor(tt.markets)

			assert.Equal(t, tt.wantPollerName, cursor.PollerName)
			assert.Equal(t, time.UTC, cursor.LastSeenAt.Location())

			if tt.wantLastSeenID != uuid.Nil {
				assert.Equal(t, tt.wantLastSeenID, cursor.LastSeenID)
			}
			if !tt.wantLastSeenAt.IsZero() {
				assert.True(t, tt.wantLastSeenAt.Equal(cursor.LastSeenAt),
					"got %v, want %v", cursor.LastSeenAt, tt.wantLastSeenAt)
			}
		})
	}
}

func TestBuildMarketStateChangedEvents(t *testing.T) {
	deletedAt := time.Now().UTC()

	tests := []struct {
		name        string
		ctx         context.Context
		markets     []sharedModels.Market
		wantLen     int
		wantErr     bool
		checkEvents func(t *testing.T, events []sharedModels.MarketStateChangedEvent, markets []sharedModels.Market)
	}{
		{
			name:    "пустой список маркетов — пустой список событий",
			ctx:     context.Background(),
			markets: []sharedModels.Market{},
			wantLen: 0,
		},
		{
			name: "маркеты конвертируются в события с правильными полями",
			ctx:  context.Background(),
			markets: []sharedModels.Market{
				{
					ID:        uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
					Enabled:   true,
					DeletedAt: nil,
					UpdatedAt: time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC),
				},
				{
					ID:        uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
					Enabled:   false,
					DeletedAt: &deletedAt,
					UpdatedAt: time.Date(2025, 3, 2, 10, 0, 0, 0, time.UTC),
				},
			},
			wantLen: 2,
			checkEvents: func(t *testing.T, events []sharedModels.MarketStateChangedEvent, markets []sharedModels.Market) {
				for i, e := range events {
					m := markets[i]
					assert.NotEqual(t, uuid.Nil, e.EventID, "EventID должен быть непустым")
					assert.Equal(t, m.ID, e.MarketID)
					assert.Equal(t, m.Enabled, e.Enabled)
					assert.Equal(t, m.DeletedAt, e.DeletedAt)
					assert.True(t, m.UpdatedAt.Equal(e.UpdatedAt))
					assert.Equal(t, time.UTC, e.UpdatedAt.Location(), "UpdatedAt должен быть в UTC")
				}
			},
		},
		{
			name: "каждое событие получает уникальный EventID",
			ctx:  context.Background(),
			markets: []sharedModels.Market{
				{ID: uuid.New(), UpdatedAt: time.Now().UTC()},
				{ID: uuid.New(), UpdatedAt: time.Now().UTC()},
				{ID: uuid.New(), UpdatedAt: time.Now().UTC()},
			},
			wantLen: 3,
			checkEvents: func(t *testing.T, events []sharedModels.MarketStateChangedEvent, _ []sharedModels.Market) {
				ids := make(map[uuid.UUID]struct{}, len(events))
				for _, e := range events {
					ids[e.EventID] = struct{}{}
				}
				assert.Len(t, ids, len(events), "все EventID должны быть уникальны")
			},
		},
		{
			name: "отменённый контекст — возвращается ошибка",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			markets: []sharedModels.Market{
				{ID: uuid.New(), UpdatedAt: time.Now().UTC()},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPoller(nil, nil, nil, nil)
			events, err := p.buildMarketStateChangedEvents(tt.ctx, tt.markets)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, events, tt.wantLen)

			if tt.checkEvents != nil {
				tt.checkEvents(t, events, tt.markets)
			}
		})
	}
}

func TestRefreshCache(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()

	tests := []struct {
		name         string
		ids          []uuid.UUID
		nilRefresher bool
		setupMocks   func(refresher *mocks.MarketCacheRefresher)
	}{
		{
			name:       "пустой список IDs — никакие методы не вызываются",
			ids:        []uuid.UUID{},
			setupMocks: func(_ *mocks.MarketCacheRefresher) {},
		},
		{
			name:         "nil cacheRefresher — нет паники, нет вызовов",
			ids:          []uuid.UUID{id1},
			nilRefresher: true,
			setupMocks:   func(_ *mocks.MarketCacheRefresher) {},
		},
		{
			name: "InvalidateByIDs и RefreshAll вызываются в правильном порядке",
			ids:  []uuid.UUID{id1, id2},
			setupMocks: func(refresher *mocks.MarketCacheRefresher) {
				call1 := refresher.On("InvalidateByIDs", mock.Anything, []uuid.UUID{id1, id2}).
					Return(nil).Once()
				refresher.On("RefreshAll", mock.Anything).
					Return(nil).Once().NotBefore(call1)
			},
		},
		{
			name: "ошибка InvalidateByIDs — не фатальна, RefreshAll всё равно вызывается",
			ids:  []uuid.UUID{id1},
			setupMocks: func(refresher *mocks.MarketCacheRefresher) {
				refresher.On("InvalidateByIDs", mock.Anything, mock.Anything).
					Return(errors.New("redis timeout")).Once()
				refresher.On("RefreshAll", mock.Anything).
					Return(nil).Once()
			},
		},
		{
			name: "ошибка RefreshAll — не фатальна, вызов не паникует",
			ids:  []uuid.UUID{id1},
			setupMocks: func(refresher *mocks.MarketCacheRefresher) {
				refresher.On("InvalidateByIDs", mock.Anything, mock.Anything).
					Return(nil).Once()
				refresher.On("RefreshAll", mock.Anything).
					Return(errors.New("redis oom")).Once()
			},
		},
		{
			name: "context.Canceled от InvalidateByIDs — RefreshAll всё равно вызывается",
			ids:  []uuid.UUID{id1},
			setupMocks: func(refresher *mocks.MarketCacheRefresher) {
				refresher.On("InvalidateByIDs", mock.Anything, mock.Anything).
					Return(context.Canceled).Once()
				refresher.On("RefreshAll", mock.Anything).
					Return(nil).Once()
			},
		},
		{
			name: "context.DeadlineExceeded от RefreshAll — не фатальна",
			ids:  []uuid.UUID{id1},
			setupMocks: func(refresher *mocks.MarketCacheRefresher) {
				refresher.On("InvalidateByIDs", mock.Anything, mock.Anything).
					Return(nil).Once()
				refresher.On("RefreshAll", mock.Anything).
					Return(context.DeadlineExceeded).Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mocks.MarketReader{}
			producer := &mocks.MarketEventProducer{}
			cursorStore := &mocks.CursorStore{}
			refresher := &mocks.MarketCacheRefresher{}
			tt.setupMocks(refresher)

			var p *MarketPoller
			if tt.nilRefresher {
				p = newTestPoller(reader, producer, cursorStore, nil)
			} else {
				p = newTestPoller(reader, producer, cursorStore, refresher)
			}

			p.refreshCache(context.Background(), tt.ids)

			refresher.AssertExpectations(t)
		})
	}
}

func TestPoll(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer, refresher *mocks.MarketCacheRefresher)
		checkErr   func(t *testing.T, err error)
		checkState func(t *testing.T, p *MarketPoller)
	}{
		{
			name: "пустой репо — нет событий, нет обновления курсора",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, nil)
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
			checkState: func(t *testing.T, p *MarketPoller) {
				assert.Equal(t, uuid.Nil, p.lastSeenID)
				assert.True(t, p.lastSeenAt.IsZero())
			},
		},
		{
			name: "один батч меньше batchSize — один вызов reader, курсор обновлён",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer, refresher *mocks.MarketCacheRefresher) {
				markets := makeMarketsForMarketPoller(2)
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(markets, nil).Once()
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(nil).Once()
				refresher.On(
					"InvalidateByIDs",
					mock.Anything,
					mock.MatchedBy(func(ids []uuid.UUID) bool {
						return len(ids) == 2 &&
							ids[0] == markets[0].ID &&
							ids[1] == markets[1].ID
					}),
				).Return(nil).Once()

				refresher.On("RefreshAll", mock.Anything).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			checkState: func(t *testing.T, p *MarketPoller) {
				assert.NotEqual(t, uuid.Nil, p.lastSeenID)
			},
		},
		{
			name: "два батча: первый полный (hasMore=true), второй пустой (hasMore=false)",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer, refresher *mocks.MarketCacheRefresher) {
				batch1 := makeMarketsForMarketPoller(testBatchSize)
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(batch1, nil).Once()
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(nil).Once()

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, nil).Once()

				refresher.On("InvalidateByIDs", mock.Anything,
					mock.MatchedBy(func(ids []uuid.UUID) bool { return len(ids) == testBatchSize }),
				).Return(nil).Once()
				refresher.On("RefreshAll", mock.Anything).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "два полных батча, потом один пустой — reader вызывается трижды",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer, refresher *mocks.MarketCacheRefresher) {
				for i := 0; i < 2; i++ {
					batch := makeMarketsForMarketPoller(testBatchSize)
					reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
						Return(batch, nil).Once()
					producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
						Return(nil).Once()
				}
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, nil).Once()

				refresher.On("InvalidateByIDs", mock.Anything,
					mock.MatchedBy(func(ids []uuid.UUID) bool { return len(ids) == testBatchSize*2 }),
				).Return(nil).Once()
				refresher.On("RefreshAll", mock.Anything).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "ошибка reader — poll возвращает ошибку, refreshCache с пустыми IDs (no-op)",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, errors.New("db error"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "db error")
			},
		},
		{
			name: "ошибка reader после успешного батча — refreshCache вызывается с уже накопленными IDs",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer, refresher *mocks.MarketCacheRefresher) {
				batch1 := makeMarketsForMarketPoller(testBatchSize)
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(batch1, nil).Once()
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(nil).Once()

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, errors.New("db error")).Once()

				refresher.On("InvalidateByIDs", mock.Anything,
					mock.MatchedBy(func(ids []uuid.UUID) bool { return len(ids) == testBatchSize }),
				).Return(nil).Once()
				refresher.On("RefreshAll", mock.Anything).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "db error")
			},
		},
		{
			name: "ошибка producer — poll возвращает ошибку",
			setupMocks: func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(makeMarketsForMarketPoller(1), nil)
				producer.On("PublishMarketStateChanged", mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("kafka down"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "kafka down")
			},
		},
		{
			name: "отменённый контекст — poll возвращает context.Canceled",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, context.Canceled)
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.ErrorIs(t, err, context.Canceled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mocks.MarketReader{}
			producer := &mocks.MarketEventProducer{}
			cursorStore := &mocks.CursorStore{}
			refresher := &mocks.MarketCacheRefresher{}
			tt.setupMocks(reader, producer, refresher)

			p := newTestPoller(reader, producer, cursorStore, refresher)
			err := p.poll(context.Background())

			tt.checkErr(t, err)
			if tt.checkState != nil {
				tt.checkState(t, p)
			}

			reader.AssertExpectations(t)
			producer.AssertExpectations(t)
			cursorStore.AssertExpectations(t)
			refresher.AssertExpectations(t)
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(reader *mocks.MarketReader, producer *mocks.MarketEventProducer, refresher *mocks.MarketCacheRefresher) context.Context
		checkErr   func(t *testing.T, err error)
	}{
		{
			name: "nil ctx — ошибка без паники",
			setupMocks: func(_ *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) context.Context {
				return nil
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "nil context")
			},
		},
		{
			name: "контекст отменяется после начального пустого poll — Run возвращает nil",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) context.Context {
				ctx, cancel := context.WithCancel(context.Background())

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, nil).Maybe()

				cancel()
				return ctx
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "ошибка в начальном poll (не отменённый ctx) — Run возвращает ошибку с префиксом",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) context.Context {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, errors.New("initial db error"))
				return context.Background()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "initial poll")
				assert.Contains(t, err.Error(), "initial db error")
			},
		},
		{
			name: "ошибка в начальном poll, но ctx уже отменён — Run возвращает nil",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, context.Canceled).Maybe()
				return ctx
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "ошибка в плановом poll (не отменённый ctx) — Run возвращает ошибку с префиксом",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) context.Context {
				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, nil).Once()

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, errors.New("scheduled db error")).Once()
				return context.Background()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "scheduled poll")
				assert.Contains(t, err.Error(), "scheduled db error")
			},
		},
		{
			name: "ошибка в плановом poll, но ctx уже отменён — Run возвращает nil",
			setupMocks: func(reader *mocks.MarketReader, _ *mocks.MarketEventProducer, _ *mocks.MarketCacheRefresher) context.Context {
				ctx, cancel := context.WithCancel(context.Background())

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Return(nil, nil).Once()

				reader.On("ListUpdatedSince", mock.Anything, mock.Anything, mock.Anything, testBatchSize).
					Run(func(_ mock.Arguments) { cancel() }).
					Return(nil, context.Canceled).Maybe()
				return ctx
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mocks.MarketReader{}
			producer := &mocks.MarketEventProducer{}
			cursorStore := &mocks.CursorStore{}
			refresher := &mocks.MarketCacheRefresher{}

			ctx := tt.setupMocks(reader, producer, refresher)

			p := newTestPoller(reader, producer, cursorStore, refresher)

			p.pollInterval = 5 * time.Millisecond

			err := p.Run(ctx)
			tt.checkErr(t, err)

			reader.AssertExpectations(t)
			producer.AssertExpectations(t)
		})
	}
}
