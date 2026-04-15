package spot

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/go-faster/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/mocks"
)

const (
	testDefaultLimit = uint64(20)
	testMaxLimit     = uint64(100)
	testCacheLimit   = uint64(50)
	testServiceName  = "spot-test"
)

var (
	testCacheTTL = 5 * time.Second
	testTimeout  = 10 * time.Second
)

func newTestViewer(
	repo *mocks.MarketRepository,
	cache *mocks.MarketCacheRepository,
	byIDCache *mocks.MarketByIDCacheRepository,
) *MarketViewer {
	return NewMarketViewer(
		repo,
		cache,
		byIDCache,
		testCacheTTL,
		testTimeout,
		testDefaultLimit,
		testMaxLimit,
		testCacheLimit,
		testServiceName,
		zapLogger.NewNop(),
	)
}

func ctxWithRoles(roles ...models.UserRole) context.Context {
	ctx, _ := requestctx.ContextWithUserRoles(context.Background(), roles)
	return ctx
}

func makeMarkets(n int) []models.Market {
	markets := make([]models.Market, n)
	for i := range markets {
		markets[i] = models.Market{
			ID:      uuid.New(),
			Name:    gofakeit.Company(),
			Enabled: true,
		}
	}
	return markets
}

func TestViewMarkets(t *testing.T) {
	gofakeit.Seed(time.Now().UnixNano())

	tests := []struct {
		name        string
		ctx         context.Context
		limit       uint64
		offset      uint64
		setupMocks  func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository)
		wantHasMore bool
		wantOffset  uint64
		wantErr     error
		checkErr    func(t *testing.T, err error)
		checkResult func(t *testing.T, markets []models.Market)
	}{
		{
			name:       "нет роли в контексте — ErrUserRoleNotSpecified",
			ctx:        context.Background(),
			limit:      10,
			offset:     0,
			setupMocks: func(_ *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {},
			wantErr:    serviceErrors.ErrUserRoleNotSpecified,
		},
		{
			name:       "пустой срез ролей — ErrUserRoleNotSpecified",
			ctx:        ctxWithRoles(),
			limit:      10,
			offset:     0,
			setupMocks: func(_ *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {},
			wantErr:    serviceErrors.ErrUserRoleNotSpecified,
		},
		{
			name:       "неизвестная роль — ErrUserRoleNotSpecified",
			ctx:        ctxWithRoles(models.UserRoleUnspecified),
			limit:      10,
			offset:     0,
			setupMocks: func(_ *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {},
			wantErr:    serviceErrors.ErrUserRoleNotSpecified,
		},
		{
			name:   "limit=0 заменяется на defaultLimit",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  0,
			offset: 5,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testDefaultLimit, uint64(5)).
					Return(makeMarkets(3), nil)
			},
		},
		{
			name:   "limit > maxLimit — обрезается до maxLimit",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  testMaxLimit + 50,
			offset: 5,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testMaxLimit, uint64(5)).
					Return(makeMarkets(5), nil)
			},
		},
		{
			name:       "offset > math.MaxInt — ErrInvalidPagination",
			ctx:        ctxWithRoles(models.UserRoleAdmin),
			limit:      10,
			offset:     math.MaxUint64,
			setupMocks: func(_ *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {},
			wantErr:    serviceErrors.ErrInvalidPagination,
		},
		{
			name:   "cache hit — head page возвращается напрямую, hasMore=true",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  10,
			offset: 0,
			setupMocks: func(_ *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetMarkets", mock.Anything, roleAdminKey).
					Return(makeMarkets(30), nil)
			},
			wantHasMore: true,
			wantOffset:  10,
		},
		{
			name:   "cache hit — ровно limit записей, hasMore=false",
			ctx:    ctxWithRoles(models.UserRoleUser),
			limit:  10,
			offset: 0,
			setupMocks: func(_ *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetMarkets", mock.Anything, roleUserKey).
					Return(makeMarkets(10), nil)
			},
			wantHasMore: false,
			wantOffset:  0,
		},
		{
			name:   "cache miss (ErrMarketsNotFound) — запрос в репо, прогрев кэша",
			ctx:    ctxWithRoles(models.UserRoleViewer),
			limit:  10,
			offset: 0,
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetMarkets", mock.Anything, roleViewerKey).
					Return(nil, repositoryErrors.ErrMarketsNotFound)

				repoMarkets := makeMarkets(int(testCacheLimit) + 1)
				repo.On("GetMarketsPage", mock.Anything, roleViewerKey, testCacheLimit+1, uint64(0)).
					Return(repoMarkets, nil)
				cache.On("SetMarkets", mock.Anything, repoMarkets, roleViewerKey, testCacheTTL).
					Return(nil)
			},
			wantHasMore: true,
			wantOffset:  10,
		},
		{
			name:   "cache miss (ErrMarketCacheCorrupted) — запрос в репо, прогрев кэша",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  5,
			offset: 0,
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetMarkets", mock.Anything, roleAdminKey).
					Return(nil, repositoryErrors.ErrMarketCacheCorrupted)

				repoMarkets := makeMarkets(5)
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testCacheLimit+1, uint64(0)).
					Return(repoMarkets, nil)
				cache.On("SetMarkets", mock.Anything, repoMarkets, roleAdminKey, testCacheTTL).
					Return(nil)
			},
			wantHasMore: false,
			wantOffset:  0,
		},
		{
			name:   "cache miss + ошибка прогрева кэша — данные всё равно возвращаются",
			ctx:    ctxWithRoles(models.UserRoleUser),
			limit:  10,
			offset: 0,
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetMarkets", mock.Anything, roleUserKey).
					Return(nil, repositoryErrors.ErrMarketsNotFound)

				repoMarkets := makeMarkets(3)
				repo.On("GetMarketsPage", mock.Anything, roleUserKey, testCacheLimit+1, uint64(0)).
					Return(repoMarkets, nil)
				cache.On("SetMarkets", mock.Anything, repoMarkets, roleUserKey, testCacheTTL).
					Return(errors.New("redis unavailable"))
			},
			wantHasMore: false,
			wantOffset:  0,
		},
		{
			name:   "cache miss + пустой репо — ErrMarketsNotFound",
			ctx:    ctxWithRoles(models.UserRoleUser),
			limit:  10,
			offset: 0,
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetMarkets", mock.Anything, roleUserKey).
					Return(nil, repositoryErrors.ErrMarketsNotFound)
				repo.On("GetMarketsPage", mock.Anything, roleUserKey, testCacheLimit+1, uint64(0)).
					Return(nil, repositoryErrors.ErrMarketStoreIsEmpty)
			},
			wantErr: serviceErrors.ErrMarketsNotFound,
		},
		{
			name:   "cache miss + репо недоступен — ошибка пробрасывается",
			ctx:    ctxWithRoles(models.UserRoleUser),
			limit:  10,
			offset: 0,
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetMarkets", mock.Anything, roleUserKey).
					Return(nil, repositoryErrors.ErrMarketsNotFound)
				repo.On("GetMarketsPage", mock.Anything, roleUserKey, testCacheLimit+1, uint64(0)).
					Return(nil, errors.New("connection refused"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "connection refused")
			},
		},
		{
			name:   "offset>0 — кэш не трогается, запрос в репо с правильными параметрами",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  10,
			offset: 20,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, uint64(10), uint64(20)).
					Return(makeMarkets(11), nil)
			},
			wantHasMore: true,
			wantOffset:  30,
		},
		{
			name:   "limit > cacheLimit — кэш не трогается, запрос в репо",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  testCacheLimit + 1,
			offset: 0,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testCacheLimit+1, uint64(0)).
					Return(makeMarkets(10), nil)
			},
		},
		{
			name:   "прямой путь, репо пустой — ErrMarketsNotFound",
			ctx:    ctxWithRoles(models.UserRoleUser),
			limit:  10,
			offset: 20,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleUserKey, uint64(10), uint64(20)).
					Return(nil, repositoryErrors.ErrMarketStoreIsEmpty)
			},
			wantErr: serviceErrors.ErrMarketsNotFound,
		},
		{
			name:   "прямой путь, репо недоступен — ошибка пробрасывается",
			ctx:    ctxWithRoles(models.UserRoleUser),
			limit:  10,
			offset: 20,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleUserKey, uint64(10), uint64(20)).
					Return(nil, errors.New("db error"))
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "db error")
			},
		},
		{
			name:   "репо вернул limit+1 элементов — hasMore=true, nextOffset=offset+limit",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  5,
			offset: 10,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, uint64(5), uint64(10)).
					Return(makeMarkets(6), nil)
			},
			wantHasMore: true,
			wantOffset:  15,
			checkResult: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 5, "должно быть обрезано до limit")
			},
		},
		{
			name:   "репо вернул ровно limit элементов — hasMore=false, nextOffset=0",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			limit:  5,
			offset: 10,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, uint64(5), uint64(10)).
					Return(makeMarkets(5), nil)
			},
			wantHasMore: false,
			wantOffset:  0,
		},
		{
			name:   "admin + user — запрос идёт с ключом admin",
			ctx:    ctxWithRoles(models.UserRoleAdmin, models.UserRoleUser),
			limit:  5,
			offset: 5,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, uint64(5), uint64(5)).
					Return(makeMarkets(2), nil)
			},
		},
		{
			name:   "viewer + user — запрос идёт с ключом viewer",
			ctx:    ctxWithRoles(models.UserRoleViewer, models.UserRoleUser),
			limit:  5,
			offset: 5,
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleViewerKey, uint64(5), uint64(5)).
					Return(makeMarkets(2), nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mocks.MarketRepository{}
			cache := &mocks.MarketCacheRepository{}
			byIDCache := &mocks.MarketByIDCacheRepository{}
			tt.setupMocks(repo, cache)

			svc := newTestViewer(repo, cache, byIDCache)

			markets, nextOffset, hasMore, err := svc.ViewMarkets(tt.ctx, tt.limit, tt.offset)

			if tt.checkErr != nil {
				tt.checkErr(t, err)
			} else if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, markets)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantHasMore, hasMore)
				assert.Equal(t, tt.wantOffset, nextOffset)
			}

			if tt.checkResult != nil && err == nil {
				tt.checkResult(t, markets)
			}

			repo.AssertExpectations(t)
			cache.AssertExpectations(t)
			byIDCache.AssertExpectations(t)
		})
	}
}

func TestGetMarketByID(t *testing.T) {
	gofakeit.Seed(time.Now().UnixNano())

	deletedAt := time.Now().UTC()

	activeMarket := models.Market{
		ID: uuid.New(), Name: gofakeit.Company(), Enabled: true, DeletedAt: nil,
	}
	disabledMarket := models.Market{
		ID: uuid.New(), Name: gofakeit.Company(), Enabled: false, DeletedAt: nil,
	}
	deletedEnabledMarket := models.Market{
		ID: uuid.New(), Name: gofakeit.Company(), Enabled: true, DeletedAt: &deletedAt,
	}
	deletedDisabledMarket := models.Market{
		ID: uuid.New(), Name: gofakeit.Company(), Enabled: false, DeletedAt: &deletedAt,
	}

	tests := []struct {
		name       string
		ctx        context.Context
		market     models.Market
		setupMocks func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository)
		wantMarket *models.Market
		wantErr    error
		checkErr   func(t *testing.T, err error)
	}{
		{
			name:       "нет роли в контексте — ErrUserRoleNotSpecified",
			ctx:        context.Background(),
			market:     activeMarket,
			setupMocks: func(_ *mocks.MarketRepository, _ *mocks.MarketByIDCacheRepository) {},
			wantErr:    serviceErrors.ErrUserRoleNotSpecified,
		},
		{
			name:       "неизвестная роль — ErrUserRoleNotSpecified",
			ctx:        ctxWithRoles(models.UserRoleUnspecified),
			market:     activeMarket,
			setupMocks: func(_ *mocks.MarketRepository, _ *mocks.MarketByIDCacheRepository) {},
			wantErr:    serviceErrors.ErrUserRoleNotSpecified,
		},
		{
			name:   "cache hit — репо не вызывается, маркет возвращается из кэша",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(activeMarket, nil).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "cache miss (ErrMarketNotFound) — load from repo, warm cache",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()

				repo.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(activeMarket, nil).Once()
				byIDCache.On("SetMarketByID", mock.Anything, activeMarket, testCacheTTL).
					Return(nil).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "double-check внутри singleflight даёт hit — репо не вызывается",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(activeMarket, nil).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "corrupted cache — load from repo, SetMarketByID успешен",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketCacheCorrupted).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketCacheCorrupted).Once()

				repo.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(activeMarket, nil).Once()
				byIDCache.On("SetMarketByID", mock.Anything, activeMarket, testCacheTTL).
					Return(nil).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "corrupted cache — SetMarketByID fails — удаляем stale ключ, маркет всё равно возвращается",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketCacheCorrupted).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketCacheCorrupted).Once()

				repo.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(activeMarket, nil).Once()
				byIDCache.On("SetMarketByID", mock.Anything, activeMarket, testCacheTTL).
					Return(errors.New("redis timeout")).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, activeMarket.ID).
					Return(nil).Once()
			},
			wantMarket: &activeMarket,
			wantErr:    nil,
			checkErr:   nil,
		},
		{
			name:   "corrupted cache — Set и Delete оба падают — маркет всё равно возвращается",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketCacheCorrupted).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketCacheCorrupted).Once()

				repo.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(activeMarket, nil).Once()
				byIDCache.On("SetMarketByID", mock.Anything, activeMarket, testCacheTTL).
					Return(errors.New("redis timeout")).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, activeMarket.ID).
					Return(errors.New("redis timeout")).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "обычная ошибка Set (не corrupted) — stale ключ не удаляется, маркет возвращается",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()

				repo.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(activeMarket, nil).Once()
				byIDCache.On("SetMarketByID", mock.Anything, activeMarket, testCacheTTL).
					Return(errors.New("redis flaky")).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "repo — ErrMarketNotFound — возвращаем ErrMarketNotFound с ID",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
				repo.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrMarketNotFound
				assert.ErrorAs(t, err, &notFound)
				assert.Equal(t, activeMarket.ID, notFound.ID)
			},
		},
		{
			name:   "repo — неизвестная ошибка — ошибка пробрасывается",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(repo *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, repositoryErrors.ErrMarketNotFound).Once()
				repo.On("GetMarketByID", mock.Anything, activeMarket.ID).
					Return(models.Market{}, errors.New("pg: timeout")).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "pg: timeout")
			},
		},
		{
			name:   "admin видит active enabled маркет",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: activeMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).Return(activeMarket, nil).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "admin видит disabled маркет",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: disabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, disabledMarket.ID).Return(disabledMarket, nil).Once()
			},
			wantMarket: &disabledMarket,
		},
		{
			name:   "admin видит deleted маркет",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: deletedEnabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, deletedEnabledMarket.ID).Return(deletedEnabledMarket, nil).Once()
			},
			wantMarket: &deletedEnabledMarket,
		},
		{
			name:   "admin видит deleted+disabled маркет",
			ctx:    ctxWithRoles(models.UserRoleAdmin),
			market: deletedDisabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, deletedDisabledMarket.ID).Return(deletedDisabledMarket, nil).Once()
			},
			wantMarket: &deletedDisabledMarket,
		},
		{
			name:   "viewer видит active маркет",
			ctx:    ctxWithRoles(models.UserRoleViewer),
			market: activeMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).Return(activeMarket, nil).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "viewer видит disabled (not deleted) маркет",
			ctx:    ctxWithRoles(models.UserRoleViewer),
			market: disabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, disabledMarket.ID).Return(disabledMarket, nil).Once()
			},
			wantMarket: &disabledMarket,
		},
		{
			name:   "viewer не видит deleted enabled маркет — ErrMarketNotFound с ID",
			ctx:    ctxWithRoles(models.UserRoleViewer),
			market: deletedEnabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, deletedEnabledMarket.ID).Return(deletedEnabledMarket, nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrMarketNotFound
				assert.ErrorAs(t, err, &notFound)
				assert.Equal(t, deletedEnabledMarket.ID, notFound.ID)
			},
		},
		{
			name:   "viewer не видит deleted+disabled маркет — ErrMarketNotFound",
			ctx:    ctxWithRoles(models.UserRoleViewer),
			market: deletedDisabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, deletedDisabledMarket.ID).Return(deletedDisabledMarket, nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrMarketNotFound
				assert.ErrorAs(t, err, &notFound)
			},
		},
		{
			name:   "user видит active enabled маркет",
			ctx:    ctxWithRoles(models.UserRoleUser),
			market: activeMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, activeMarket.ID).Return(activeMarket, nil).Once()
			},
			wantMarket: &activeMarket,
		},
		{
			name:   "user не видит disabled маркет — ErrDisabled с ID",
			ctx:    ctxWithRoles(models.UserRoleUser),
			market: disabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, disabledMarket.ID).Return(disabledMarket, nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var disabled serviceErrors.ErrDisabled
				assert.ErrorAs(t, err, &disabled)
				assert.Equal(t, disabledMarket.ID, disabled.ID)
			},
		},
		{
			name:   "user не видит deleted enabled маркет — ErrMarketNotFound (deleted > enabled)",
			ctx:    ctxWithRoles(models.UserRoleUser),
			market: deletedEnabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, deletedEnabledMarket.ID).Return(deletedEnabledMarket, nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrMarketNotFound
				assert.ErrorAs(t, err, &notFound)
				assert.Equal(t, deletedEnabledMarket.ID, notFound.ID)
			},
		},
		{
			name:   "user не видит deleted+disabled маркет — ErrMarketNotFound (deleted проверяется первым)",
			ctx:    ctxWithRoles(models.UserRoleUser),
			market: deletedDisabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, deletedDisabledMarket.ID).Return(deletedDisabledMarket, nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				var notFound sharedErrors.ErrMarketNotFound
				assert.ErrorAs(t, err, &notFound)
			},
		},
		{
			name:   "admin + user — admin, deleted маркет виден",
			ctx:    ctxWithRoles(models.UserRoleAdmin, models.UserRoleUser),
			market: deletedEnabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, deletedEnabledMarket.ID).Return(deletedEnabledMarket, nil).Once()
			},
			wantMarket: &deletedEnabledMarket,
		},
		{
			name:   "viewer + user — viewer, disabled маркет виден",
			ctx:    ctxWithRoles(models.UserRoleViewer, models.UserRoleUser),
			market: disabledMarket,
			setupMocks: func(_ *mocks.MarketRepository, byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("GetMarketByID", mock.Anything, disabledMarket.ID).Return(disabledMarket, nil).Once()
			},
			wantMarket: &disabledMarket,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mocks.MarketRepository{}
			cache := &mocks.MarketCacheRepository{}
			byIDCache := &mocks.MarketByIDCacheRepository{}
			tt.setupMocks(repo, byIDCache)

			svc := newTestViewer(repo, cache, byIDCache)

			got, err := svc.GetMarketByID(tt.ctx, tt.market.ID)

			if tt.checkErr != nil {
				tt.checkErr(t, err)
			} else if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, tt.wantMarket)
				assert.Equal(t, tt.wantMarket.ID, got.ID)
				assert.Equal(t, tt.wantMarket.Enabled, got.Enabled)
			}

			repo.AssertExpectations(t)
			cache.AssertExpectations(t)
			byIDCache.AssertExpectations(t)
		})
	}
}

func TestRefreshAll(t *testing.T) {
	allRoles := []string{roleAdminKey, roleViewerKey, roleUserKey}

	tests := []struct {
		name       string
		useNilCtx  bool
		setupMocks func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository)
		checkErr   func(t *testing.T, err error)
	}{
		{
			name: "успешное обновление кэша для всех трёх ролей",
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				for _, role := range allRoles {
					markets := makeMarkets(3)
					repo.On("GetMarketsPage", mock.Anything, role, testCacheLimit+1, uint64(0)).
						Return(markets, nil).Once()
					cache.On("SetMarkets", mock.Anything, markets, role, testCacheTTL).
						Return(nil).Once()
				}
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "пустой store — инвалидируем кэши всех трёх ролей",
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testCacheLimit+1, uint64(0)).
					Return(nil, repositoryErrors.ErrMarketStoreIsEmpty).Once()
				for _, role := range allRoles {
					cache.On("DeleteMarkets", mock.Anything, role).Return(nil).Once()
				}
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "пустой store + DeleteMarkets падает — ошибка",
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testCacheLimit+1, uint64(0)).
					Return(nil, repositoryErrors.ErrMarketStoreIsEmpty).Once()
				cache.On("DeleteMarkets", mock.Anything, roleAdminKey).
					Return(errors.New("redis down")).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "redis down")
			},
		},
		{
			name: "GetMarketsPage падает — ошибка, следующие роли не обрабатываются",
			setupMocks: func(repo *mocks.MarketRepository, _ *mocks.MarketCacheRepository) {
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testCacheLimit+1, uint64(0)).
					Return(nil, errors.New("pg timeout")).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "pg timeout")
			},
		},
		{
			name: "SetMarkets падает — ошибка",
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				markets := makeMarkets(2)
				repo.On("GetMarketsPage", mock.Anything, roleAdminKey, testCacheLimit+1, uint64(0)).
					Return(markets, nil).Once()
				cache.On("SetMarkets", mock.Anything, markets, roleAdminKey, testCacheTTL).
					Return(errors.New("redis oom")).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "redis oom")
			},
		},
		{
			name:      "nil ctx — не паникует",
			useNilCtx: true,
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				for _, role := range allRoles {
					markets := makeMarkets(1)
					repo.On("GetMarketsPage", mock.Anything, role, testCacheLimit+1, uint64(0)).
						Return(markets, nil).Once()
					cache.On("SetMarkets", mock.Anything, markets, role, testCacheTTL).
						Return(nil).Once()
				}
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mocks.MarketRepository{}
			cache := &mocks.MarketCacheRepository{}
			byIDCache := &mocks.MarketByIDCacheRepository{}
			tt.setupMocks(repo, cache)

			svc := newTestViewer(repo, cache, byIDCache)

			var ctx context.Context
			if !tt.useNilCtx {
				ctx = context.Background()
			}

			err := svc.RefreshAll(ctx)
			tt.checkErr(t, err)

			repo.AssertExpectations(t)
			cache.AssertExpectations(t)
			byIDCache.AssertExpectations(t)
		})
	}
}

func TestInvalidateByIDs(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	id3 := uuid.New()

	tests := []struct {
		name       string
		ids        []uuid.UUID
		setupMocks func(byIDCache *mocks.MarketByIDCacheRepository)
		checkErr   func(t *testing.T, err error)
	}{
		{
			name:       "nil срез — nil без паники",
			ids:        nil,
			setupMocks: func(_ *mocks.MarketByIDCacheRepository) {},
			checkErr:   func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:       "пустой срез — nil, моки не вызываются",
			ids:        []uuid.UUID{},
			setupMocks: func(_ *mocks.MarketByIDCacheRepository) {},
			checkErr:   func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "один ID — успешное удаление",
			ids:  []uuid.UUID{id1},
			setupMocks: func(byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("DeleteMarketByID", mock.Anything, id1).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "несколько уникальных ID — каждый удаляется ровно один раз",
			ids:  []uuid.UUID{id1, id2, id3},
			setupMocks: func(byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("DeleteMarketByID", mock.Anything, id1).Return(nil).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, id2).Return(nil).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, id3).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "дублирующиеся ID — дедупликация, каждый удаляется ровно один раз",
			ids:  []uuid.UUID{id1, id2, id1, id2, id1},
			setupMocks: func(byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("DeleteMarketByID", mock.Anything, id1).Return(nil).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, id2).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "ошибка при удалении одного ID — ошибка содержит его UUID",
			ids:  []uuid.UUID{id1},
			setupMocks: func(byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("DeleteMarketByID", mock.Anything, id1).
					Return(errors.New("redis timeout")).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), id1.String())
			},
		},
		{
			name: "ошибки при удалении нескольких ID — все ошибки объединяются",
			ids:  []uuid.UUID{id1, id2},
			setupMocks: func(byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("DeleteMarketByID", mock.Anything, id1).
					Return(errors.New("err1")).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, id2).
					Return(errors.New("err2")).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "err1")
				assert.Contains(t, err.Error(), "err2")
			},
		},
		{
			name: "частичная ошибка — успешные удаляются, ошибочные попадают в результат",
			ids:  []uuid.UUID{id1, id2, id3},
			setupMocks: func(byIDCache *mocks.MarketByIDCacheRepository) {
				byIDCache.On("DeleteMarketByID", mock.Anything, id1).Return(nil).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, id2).
					Return(errors.New("redis timeout")).Once()
				byIDCache.On("DeleteMarketByID", mock.Anything, id3).Return(nil).Once()
			},
			checkErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), id2.String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mocks.MarketRepository{}
			cache := &mocks.MarketCacheRepository{}
			byIDCache := &mocks.MarketByIDCacheRepository{}
			tt.setupMocks(byIDCache)

			svc := newTestViewer(repo, cache, byIDCache)
			err := svc.InvalidateByIDs(context.Background(), tt.ids)
			tt.checkErr(t, err)

			repo.AssertExpectations(t)
			cache.AssertExpectations(t)
			byIDCache.AssertExpectations(t)
		})
	}
}

func TestBuildPageResponse(t *testing.T) {
	markets5 := makeMarkets(5)

	tests := []struct {
		name        string
		markets     []models.Market
		limit       uint64
		offset      uint64
		wantLen     int
		wantHasMore bool
		wantOffset  uint64
		wantErr     bool
	}{
		{
			name:        "markets > limit — trim до limit, hasMore=true",
			markets:     markets5,
			limit:       3,
			offset:      0,
			wantLen:     3,
			wantHasMore: true,
			wantOffset:  3,
		},
		{
			name:        "markets == limit — нет trim, hasMore=false, nextOffset=0",
			markets:     markets5,
			limit:       5,
			offset:      10,
			wantLen:     5,
			wantHasMore: false,
			wantOffset:  0,
		},
		{
			name:        "markets < limit — нет trim, hasMore=false",
			markets:     markets5,
			limit:       10,
			offset:      0,
			wantLen:     5,
			wantHasMore: false,
			wantOffset:  0,
		},
		{
			name:        "пустой список — hasMore=false, nextOffset=0",
			markets:     []models.Market{},
			limit:       10,
			offset:      0,
			wantLen:     0,
			wantHasMore: false,
			wantOffset:  0,
		},
		{
			name:    "overflow nextOffset — ErrInvalidPagination",
			markets: markets5,
			limit:   3,
			offset:  math.MaxUint64 - 2,
			wantErr: true,
		},
		{
			name:        "nextOffset ровно на границе MaxUint64 — не overflow",
			markets:     markets5,
			limit:       3,
			offset:      math.MaxUint64 - 3,
			wantLen:     3,
			wantHasMore: true,
			wantOffset:  math.MaxUint64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, nextOffset, hasMore, err := buildPageResponse(tt.markets, tt.limit, tt.offset)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, serviceErrors.ErrInvalidPagination)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
			assert.Equal(t, tt.wantHasMore, hasMore)
			assert.Equal(t, tt.wantOffset, nextOffset)
		})
	}
}

func TestNormalizeLimit(t *testing.T) {
	tests := []struct {
		name         string
		limit        uint64
		defaultLimit uint64
		maxLimit     uint64
		want         uint64
	}{
		{name: "0 → defaultLimit", limit: 0, defaultLimit: 20, maxLimit: 100, want: 20},
		{name: "в пределах — без изменений", limit: 50, defaultLimit: 20, maxLimit: 100, want: 50},
		{name: "равен maxLimit — без изменений", limit: 100, defaultLimit: 20, maxLimit: 100, want: 100},
		{name: "превышает maxLimit — обрезается до maxLimit", limit: 200, defaultLimit: 20, maxLimit: 100, want: 100},
		{name: "limit=1 — минимальный валидный", limit: 1, defaultLimit: 20, maxLimit: 100, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLimit(tt.limit, tt.defaultLimit, tt.maxLimit)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEffectiveUserRole(t *testing.T) {
	tests := []struct {
		name    string
		roles   []models.UserRole
		wantKey string
		wantOk  bool
	}{
		{name: "одна роль admin", roles: []models.UserRole{models.UserRoleAdmin}, wantKey: roleAdminKey, wantOk: true},
		{name: "одна роль viewer", roles: []models.UserRole{models.UserRoleViewer}, wantKey: roleViewerKey, wantOk: true},
		{name: "одна роль user", roles: []models.UserRole{models.UserRoleUser}, wantKey: roleUserKey, wantOk: true},
		{name: "пустой список", roles: []models.UserRole{}, wantKey: "", wantOk: false},
		{name: "только unspecified", roles: []models.UserRole{models.UserRoleUnspecified}, wantKey: "", wantOk: false},
		{name: "admin + user → admin", roles: []models.UserRole{models.UserRoleAdmin, models.UserRoleUser}, wantKey: roleAdminKey, wantOk: true},
		{name: "user + admin → admin (порядок не важен)", roles: []models.UserRole{models.UserRoleUser, models.UserRoleAdmin}, wantKey: roleAdminKey, wantOk: true},
		{name: "viewer + user → viewer", roles: []models.UserRole{models.UserRoleViewer, models.UserRoleUser}, wantKey: roleViewerKey, wantOk: true},
		{name: "unspecified + user → user", roles: []models.UserRole{models.UserRoleUnspecified, models.UserRoleUser}, wantKey: roleUserKey, wantOk: true},
		{name: "user затем viewer — viewer выигрывает", roles: []models.UserRole{models.UserRoleUser, models.UserRoleViewer}, wantKey: roleViewerKey, wantOk: true},
		{name: "admin + viewer + user → admin", roles: []models.UserRole{models.UserRoleAdmin, models.UserRoleViewer, models.UserRoleUser}, wantKey: roleAdminKey, wantOk: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotOk := effectiveUserRole(tt.roles)
			assert.Equal(t, tt.wantKey, gotKey)
			assert.Equal(t, tt.wantOk, gotOk)
		})
	}
}
