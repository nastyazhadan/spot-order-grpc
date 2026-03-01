package spot

import (
	"context"
	"errors"
	"testing"
	"time"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/mocks"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestViewMarkets(t *testing.T) {
	gofakeit.Seed(time.Now().UnixNano())

	cacheTTL := 5 * time.Second

	enabledMarket := models.Market{
		ID:        uuid.New(),
		Name:      gofakeit.Company(),
		Enabled:   true,
		DeletedAt: nil,
	}

	disabledMarket := models.Market{
		ID:        uuid.New(),
		Name:      gofakeit.Company(),
		Enabled:   false,
		DeletedAt: nil,
	}

	deletedTime := time.Now().UTC()
	deletedMarket := models.Market{
		ID:        uuid.New(),
		Name:      gofakeit.Company(),
		Enabled:   true,
		DeletedAt: &deletedTime,
	}

	deletedAndDisabledMarket := models.Market{
		ID:        uuid.New(),
		Name:      gofakeit.Company(),
		Enabled:   false,
		DeletedAt: &deletedTime,
	}

	allMarkets := []models.Market{
		enabledMarket,
		disabledMarket,
		deletedMarket,
		deletedAndDisabledMarket,
	}

	tests := []struct {
		name               string
		userRoles          []models.UserRole
		setupMocks         func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository)
		expectedMarketsNum int
		checkMarkets       func(t *testing.T, markets []models.Market)
		expectedErr        error
		checkError         func(t *testing.T, err error)
	}{
		{
			name:      "админ видит все рынки",
			userRoles: []models.UserRole{models.UserRoleAdmin},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(allMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 4,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 4)

				hasDeleted := false
				hasDisabled := false
				for _, market := range markets {
					if market.DeletedAt != nil {
						hasDeleted = true
					}
					if !market.Enabled {
						hasDisabled = true
					}
				}
				assert.True(t, hasDeleted, "Админ должен видеть удаленные рынки")
				assert.True(t, hasDisabled, "Админ должен видеть отключенные рынки")
			},
			expectedErr: nil,
		},
		{
			name:      "viewer видит все неудаленные рынки (включая disabled)",
			userRoles: []models.UserRole{models.UserRoleViewer},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(allMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 2,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 2)
				for _, market := range markets {
					assert.Nil(t, market.DeletedAt, "Viewer не должен видеть удаленные рынки")
				}
			},
			expectedErr: nil,
		},
		{
			name:      "user видит только enabled и неудаленные рынки",
			userRoles: []models.UserRole{models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(allMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 1,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 1)
				for _, market := range markets {
					assert.True(t, market.Enabled, "User должен видеть только enabled рынки")
					assert.Nil(t, market.DeletedAt, "User не должен видеть удаленные рынки")
				}
			},
			expectedErr: nil,
		},
		{
			name:      "несколько ролей - admin и user",
			userRoles: []models.UserRole{models.UserRoleAdmin, models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(allMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 4,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 4)
			},
			expectedErr: nil,
		},
		{
			name:      "несколько ролей - viewer и user",
			userRoles: []models.UserRole{models.UserRoleViewer, models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(allMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 2,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 2)
			},
			expectedErr: nil,
		},
		{
			name:      "ошибка - хранилище рынков пустое",
			userRoles: []models.UserRole{models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketStoreIsEmpty)
			},
			expectedMarketsNum: 0,
			checkMarkets:       nil,
			expectedErr:        serviceErrors.ErrMarketsNotFound,
		},
		{
			name:      "ошибка - база данных недоступна",
			userRoles: []models.UserRole{models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(nil, errors.New("internal error"))
			},
			expectedMarketsNum: 0,
			checkMarkets:       nil,
			expectedErr:        nil,
			checkError: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "Service.getMarketsFromRepo: internal error")
			},
		},
		{
			name:      "corner case - пустой список ролей",
			userRoles: []models.UserRole{},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(allMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 0,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 0)
			},
			expectedErr: nil,
		},
		{
			name:      "corner case - неизвестная роль",
			userRoles: []models.UserRole{models.UserRoleUnspecified},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(allMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 0,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 0)
			},
			expectedErr: nil,
		},
		{
			name:      "corner case - только enabled рынки в базе (user role)",
			userRoles: []models.UserRole{models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				onlyEnabled := []models.Market{enabledMarket}

				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(onlyEnabled, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 1,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 1)
				assert.True(t, markets[0].Enabled)
			},
			expectedErr: nil,
		},
		{
			name:      "corner case - только disabled рынки в базе (user role)",
			userRoles: []models.UserRole{models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				onlyDisabled := []models.Market{disabledMarket}

				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(onlyDisabled, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 0,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 0)
			},
			expectedErr: nil,
		},
		{
			name:      "corner case - только deleted рынки в базе (user role)",
			userRoles: []models.UserRole{models.UserRoleUser},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				onlyDeleted := []models.Market{deletedMarket}

				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(onlyDeleted, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 0,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 0)
			},
			expectedErr: nil,
		},
		{
			name:      "большое количество рынков - admin",
			userRoles: []models.UserRole{models.UserRoleAdmin},
			setupMocks: func(repo *mocks.MarketRepository, cache *mocks.MarketCacheRepository) {
				manyMarkets := make([]models.Market, 100)
				for i := 0; i < 100; i++ {
					manyMarkets[i] = models.Market{
						ID:      uuid.New(),
						Name:    gofakeit.Company(),
						Enabled: i%2 == 0,
						DeletedAt: func() *time.Time {
							if i%3 == 0 {
								t := time.Now().UTC()
								return &t
							}
							return nil
						}(),
					}
				}
				cache.On("GetAll", mock.Anything).
					Return(nil, repositoryErrors.ErrMarketCacheNotFound)

				repo.On("ListAll", mock.Anything).
					Return(manyMarkets, nil)

				cache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
					Return(nil)
			},
			expectedMarketsNum: 100,
			checkMarkets: func(t *testing.T, markets []models.Market) {
				assert.Len(t, markets, 100)
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockRepo := mocks.NewMarketRepository(t)
			mockCache := mocks.NewMarketCacheRepository(t)
			test.setupMocks(mockRepo, mockCache)

			service := NewService(mockRepo, mockCache, cacheTTL)
			ctx := context.Background()

			markets, err := service.ViewMarkets(ctx, test.userRoles)

			if test.checkError != nil {
				test.checkError(t, err)
			} else if test.expectedErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, test.expectedErr)
			} else {
				require.NoError(t, err)
			}

			if test.expectedMarketsNum >= 0 {
				assert.Len(t, markets, test.expectedMarketsNum)
			}

			if test.checkMarkets != nil {
				test.checkMarkets(t, markets)
			}

			mockRepo.AssertExpectations(t)
			mockCache.AssertExpectations(t)
		})
	}
}

func TestViewMarketsWithFilters(t *testing.T) {
	cacheTTL := 5 * time.Minute

	tests := []struct {
		name            string
		market          models.Market
		userRole        models.UserRole
		shouldBeVisible bool
	}{
		{
			name: "enabled, не deleted - видит User",
			market: models.Market{
				ID:        uuid.New(),
				Name:      "Market1",
				Enabled:   true,
				DeletedAt: nil,
			},
			userRole:        models.UserRoleUser,
			shouldBeVisible: true,
		},
		{
			name: "enabled, не deleted - видит Viewer",
			market: models.Market{
				ID:        uuid.New(),
				Name:      "Market2",
				Enabled:   true,
				DeletedAt: nil,
			},
			userRole:        models.UserRoleViewer,
			shouldBeVisible: true,
		},
		{
			name: "enabled, не deleted - видит Admin",
			market: models.Market{
				ID:        uuid.New(),
				Name:      "Market3",
				Enabled:   true,
				DeletedAt: nil,
			},
			userRole:        models.UserRoleAdmin,
			shouldBeVisible: true,
		},
		{
			name: "disabled, не deleted - не видит User",
			market: models.Market{
				ID:        uuid.New(),
				Name:      "Market4",
				Enabled:   false,
				DeletedAt: nil,
			},
			userRole:        models.UserRoleUser,
			shouldBeVisible: false,
		},
		{
			name: "disabled, не deleted - видит Viewer",
			market: models.Market{
				ID:        uuid.New(),
				Name:      "Market5",
				Enabled:   false,
				DeletedAt: nil,
			},
			userRole:        models.UserRoleViewer,
			shouldBeVisible: true,
		},
		{
			name: "disabled, не deleted - видит Admin",
			market: models.Market{
				ID:        uuid.New(),
				Name:      "Market6",
				Enabled:   false,
				DeletedAt: nil,
			},
			userRole:        models.UserRoleAdmin,
			shouldBeVisible: true,
		},
		{
			name: "enabled, deleted - не видит User",
			market: models.Market{
				ID:      uuid.New(),
				Name:    "Market7",
				Enabled: true,
				DeletedAt: func() *time.Time {
					t := time.Now().UTC()
					return &t
				}(),
			},
			userRole:        models.UserRoleUser,
			shouldBeVisible: false,
		},
		{
			name: "enabled, deleted - не видит Viewer",
			market: models.Market{
				ID:      uuid.New(),
				Name:    "Market8",
				Enabled: true,
				DeletedAt: func() *time.Time {
					t := time.Now().UTC()
					return &t
				}(),
			},
			userRole:        models.UserRoleViewer,
			shouldBeVisible: false,
		},
		{
			name: "enabled, deleted - видит Admin",
			market: models.Market{
				ID:      uuid.New(),
				Name:    "Market9",
				Enabled: true,
				DeletedAt: func() *time.Time {
					t := time.Now().UTC()
					return &t
				}(),
			},
			userRole:        models.UserRoleAdmin,
			shouldBeVisible: true,
		},
		{
			name: "disabled, deleted - не видит User",
			market: models.Market{
				ID:      uuid.New(),
				Name:    "Market10",
				Enabled: false,
				DeletedAt: func() *time.Time {
					t := time.Now().UTC()
					return &t
				}(),
			},
			userRole:        models.UserRoleUser,
			shouldBeVisible: false,
		},
		{
			name: "disabled, deleted - не видит Viewer",
			market: models.Market{
				ID:      uuid.New(),
				Name:    "Market11",
				Enabled: false,
				DeletedAt: func() *time.Time {
					t := time.Now().UTC()
					return &t
				}(),
			},
			userRole:        models.UserRoleViewer,
			shouldBeVisible: false,
		},
		{
			name: "disabled, deleted - видит Admin",
			market: models.Market{
				ID:      uuid.New(),
				Name:    "Market12",
				Enabled: false,
				DeletedAt: func() *time.Time {
					t := time.Now().UTC()
					return &t
				}(),
			},
			userRole:        models.UserRoleAdmin,
			shouldBeVisible: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockRepo := mocks.NewMarketRepository(t)
			mockCache := mocks.NewMarketCacheRepository(t)

			mockCache.On("GetAll", mock.Anything).
				Return(nil, repositoryErrors.ErrMarketCacheNotFound)

			mockRepo.On("ListAll", mock.Anything).
				Return([]models.Market{test.market}, nil)

			mockCache.On("SetAll", mock.Anything, mock.Anything, cacheTTL).
				Return(nil)

			service := NewService(mockRepo, mockCache, cacheTTL)
			ctx := context.Background()

			markets, err := service.ViewMarkets(ctx, []models.UserRole{test.userRole})
			require.NoError(t, err)

			if test.shouldBeVisible {
				assert.Len(t, markets, 1, "Market should be visible")
				assert.Equal(t, test.market.ID, markets[0].ID)
			} else {
				assert.Len(t, markets, 0, "Market should not be visible")
			}

			mockRepo.AssertExpectations(t)
			mockCache.AssertExpectations(t)
		})
	}
}
