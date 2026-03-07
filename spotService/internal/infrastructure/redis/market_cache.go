package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/redis"

	redisGo "github.com/redis/go-redis/v9"
)

const cacheKeyPrefix = "market:cache"

type MarketCacheRepository struct {
	redisClient cache.Client
}

func NewMarketCacheRepository(client cache.Client) *MarketCacheRepository {
	return &MarketCacheRepository{
		redisClient: client,
	}
}

func cacheKey(roleKey string) string {
	return fmt.Sprintf("%s:%s", cacheKeyPrefix, roleKey)
}

func (m *MarketCacheRepository) GetAll(
	ctx context.Context,
	roleKey string,
) ([]models.Market, error) {

	const op = "MarketCacheRepository.GetAll"

	data, err := m.redisClient.Get(ctx, cacheKey(roleKey))
	if err != nil {
		if errors.Is(err, redisGo.Nil) {
			return nil, repositoryErrors.ErrMarketCacheNotFound
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var redisViews []dto.MarketRedisView
	if err = json.Unmarshal(data, &redisViews); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	markets := make([]models.Market, 0, len(redisViews))
	for _, redisView := range redisViews {
		market, err := redisView.ToDomain()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		markets = append(markets, market)
	}

	return markets, nil
}

func (m *MarketCacheRepository) SetAll(
	ctx context.Context,
	markets []models.Market,
	roleKey string,
	ttl time.Duration,
) error {

	const op = "MarketCacheRepository.SetAll"

	redisViews := make([]dto.MarketRedisView, 0, len(markets))
	for _, market := range markets {
		redisViews = append(redisViews, dto.FromDomain(market))
	}

	data, err := json.Marshal(redisViews)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err = m.redisClient.SetWithTTL(ctx, cacheKey(roleKey), data, ttl); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
