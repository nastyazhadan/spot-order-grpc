package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/redis"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	cacheKeyPrefix   = "market:cache"
	cacheKeyIDPrefix = "market:id"
)

type MarketCacheRepository struct {
	cacheStore  *cache.Store
	serviceName string
}

func NewMarketCacheRepository(store *cache.Store, serviceName string) *MarketCacheRepository {
	return &MarketCacheRepository{
		cacheStore:  store,
		serviceName: serviceName,
	}
}

func cacheKey(roleKey string) string {
	return fmt.Sprintf("%s:%s", cacheKeyPrefix, roleKey)
}

func cacheKeyByID(id uuid.UUID) string {
	return fmt.Sprintf("%s:%s", cacheKeyIDPrefix, id)
}

func (m *MarketCacheRepository) GetAll(
	ctx context.Context,
	roleKey string,
) ([]models.Market, error) {
	const op = "redis.MarketCacheRepository.GetAll"

	ctx, span := tracing.StartSpan(ctx, "redis.get_markets",
		trace.WithAttributes(attribute.String("role_key", roleKey)),
	)
	defer span.End()

	start := time.Now()
	data, err := m.cacheStore.Get(ctx, cacheKey(roleKey))
	metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "get_all").Observe(time.Since(start).Seconds())

	if err != nil {
		if errors.Is(err, sharedErrors.ErrCacheNotFound) {
			metrics.CacheMissesTotal.WithLabelValues(m.serviceName, "get_all").Inc()
			return nil, repositoryErrors.ErrMarketsNotFound
		}

		span.RecordError(err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	metrics.CacheHitsTotal.WithLabelValues(m.serviceName, "get_all").Inc()

	var redisViews []dto.MarketRedisView
	if err = json.Unmarshal(data, &redisViews); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	markets := make([]models.Market, 0, len(redisViews))
	for _, redisView := range redisViews {
		market, mapError := redisView.ToDomain()
		if mapError != nil {
			span.RecordError(mapError)
			return nil, fmt.Errorf("%s: %w", op, mapError)
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
	const op = "redis.MarketCacheRepository.SetAll"

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

func (m *MarketCacheRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (models.Market, error) {
	const op = "redis.MarketCacheRepository.GetByID"

	ctx, span := tracing.StartSpan(ctx, "redis.get_market_by_id",
		trace.WithAttributes(attribute.String("market_id", id.String())),
	)
	defer span.End()

	start := time.Now()
	data, err := m.cacheStore.Get(ctx, cacheKeyByID(id))
	metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "get_by_id").Observe(time.Since(start).Seconds())

	if err != nil {
		if errors.Is(err, sharedErrors.ErrCacheNotFound) {
			metrics.CacheMissesTotal.WithLabelValues(m.serviceName, "get_by_id").Inc()
			return models.Market{}, repositoryErrors.ErrMarketNotFound
		}

		span.RecordError(err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	metrics.CacheHitsTotal.WithLabelValues(m.serviceName, "get_by_id").Inc()

	var redisView dto.MarketRedisView
	if err = json.Unmarshal(data, &redisView); err != nil {
		span.RecordError(err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	market, err := redisView.ToDomain()
	if err != nil {
		span.RecordError(err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	return market, nil
}

func (m *MarketCacheRepository) SetByID(
	ctx context.Context,
	market models.Market,
	ttl time.Duration,
) error {
	const op = "redis.MarketCacheRepository.SetById"

	ctx, span := tracing.StartSpan(ctx, "redis.set_market_by_id",
		trace.WithAttributes(attribute.String("market_id", market.ID.String())),
	)
	defer span.End()

	data, err := json.Marshal(dto.FromDomain(market))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("%s: %w", op, err)
	}

	if err = m.cacheStore.SetWithTTL(ctx, cacheKeyByID(market.ID), data, ttl); err != nil {
		span.RecordError(err)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
