package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/redis"

	redisGo "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const cacheKeyPrefix = "market:cache"

type MarketCacheRepository struct {
	cacheStore *cache.Store
}

func NewMarketCacheRepository(store *cache.Store) *MarketCacheRepository {
	return &MarketCacheRepository{
		cacheStore: store,
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

	ctx, span := tracing.StartSpan(ctx, "redis.get_markets",
		trace.WithAttributes(
			attribute.String("role_key", roleKey),
		),
	)
	defer span.End()

	data, err := m.cacheStore.Get(ctx, cacheKey(roleKey))
	if err != nil {
		if errors.Is(err, redisGo.Nil) {
			return nil, repositoryErrors.ErrMarketCacheNotFound
		}

		span.RecordError(err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var redisViews []dto.MarketRedisView
	if err = json.Unmarshal(data, &redisViews); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	markets := make([]models.Market, 0, len(redisViews))
	for _, redisView := range redisViews {
		market, err := redisView.ToDomain()
		if err != nil {
			span.RecordError(err)
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

	ctx, span := tracing.StartSpan(ctx, "redis.set_markets",
		trace.WithAttributes(
			attribute.String("role_key", roleKey),
			attribute.Int("markets_count", len(markets)),
			attribute.String("ttl", ttl.String()),
		),
	)
	defer span.End()

	redisViews := make([]dto.MarketRedisView, 0, len(markets))
	for _, market := range markets {
		redisViews = append(redisViews, dto.FromDomain(market))
	}

	data, err := json.Marshal(redisViews)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("%s: %w", op, err)
	}

	if err = m.cacheStore.SetWithTTL(ctx, cacheKey(roleKey), data, ttl); err != nil {
		span.RecordError(err)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
