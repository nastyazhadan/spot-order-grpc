package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redigo "github.com/gomodule/redigo/redis"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	postgres "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/dto"
	redisDTO "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis/dto"
)

const cacheKeyPrefix = "market:cache:all"

type MarketCacheRepository struct {
	cache cache.Client
}

func NewMarketCacheRepository(cache cache.Client) *MarketCacheRepository {
	return &MarketCacheRepository{
		cache: cache,
	}
}

func (m *MarketCacheRepository) GetAll(ctx context.Context) ([]models.Market, error) {
	const op = "MarketCacheRepository.GetAll"

	data, err := m.cache.Get(ctx, cacheKeyPrefix)
	if err != nil {
		if errors.Is(err, redigo.ErrNil) {
			return nil, repositoryErrors.ErrMarketCacheNotFound
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var redisViews []redisDTO.MarketRedisView
	if err := json.Unmarshal(data, &redisViews); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	markets := make([]models.Market, 0, len(redisViews))
	for _, redisView := range redisViews {
		market, err := redisView.ToDomainView()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		markets = append(markets, market)
	}

	return markets, nil
}

func (m *MarketCacheRepository) SetAll(ctx context.Context, markets []models.Market, ttl time.Duration) error {
	const op = "MarketCacheRepository.SetAll"

	redisViews := make([]redisDTO.MarketRedisView, 0, len(markets))
	for _, market := range markets {
		postgresDTO := postgres.Market{
			ID:        market.ID,
			Name:      market.Name,
			Enabled:   market.Enabled,
			DeletedAt: market.DeletedAt,
		}
		redisViews = append(redisViews, postgresDTO.ToRedisView())
	}

	data, err := json.Marshal(redisViews)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err = m.cache.SetWithTTL(ctx, cacheKeyPrefix, data, ttl); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
