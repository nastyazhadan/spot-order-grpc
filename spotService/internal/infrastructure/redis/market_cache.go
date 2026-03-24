package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/redis"
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
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("role_key", roleKey),
		),
	)
	defer span.End()

	start := time.Now()
	data, err := m.cacheStore.Get(ctx, cacheKey(roleKey))
	metrics.ObserveWithTrace(ctx,
		metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "get_all"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		if errors.Is(err, sharedErrors.ErrCacheNotFound) {
			metrics.CacheMissesTotal.WithLabelValues(m.serviceName, "get_all").Inc()
			return nil, repositoryErrors.ErrMarketsNotFound
		}

		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	metrics.CacheHitsTotal.WithLabelValues(m.serviceName, "get_all").Inc()

	var redisViews []dto.MarketRedisView
	if err = json.Unmarshal(data, &redisViews); err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	markets := make([]models.Market, 0, len(redisViews))
	for _, redisView := range redisViews {
		market, mapError := redisView.ToDomain()
		if mapError != nil {
			tracing.RecordError(span, mapError)
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
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "redis"),
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
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	start := time.Now()
	err = m.cacheStore.SetWithTTL(ctx, cacheKey(roleKey), data, ttl)
	metrics.ObserveWithTrace(ctx,
		metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "set_all"),
		time.Since(start).Seconds(),
	)
	if err != nil {
		tracing.RecordError(span, err)
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
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("market_id", id.String()),
		),
	)
	defer span.End()

	start := time.Now()
	data, err := m.cacheStore.Get(ctx, cacheKeyByID(id))
	metrics.ObserveWithTrace(ctx,
		metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "get_by_id"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		if errors.Is(err, sharedErrors.ErrCacheNotFound) {
			metrics.CacheMissesTotal.WithLabelValues(m.serviceName, "get_by_id").Inc()
			return models.Market{}, repositoryErrors.ErrMarketNotFound
		}

		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	metrics.CacheHitsTotal.WithLabelValues(m.serviceName, "get_by_id").Inc()

	var redisView dto.MarketRedisView
	if err = json.Unmarshal(data, &redisView); err != nil {
		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	market, err := redisView.ToDomain()
	if err != nil {
		tracing.RecordError(span, err)
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
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("market_id", market.ID.String()),
		),
	)
	defer span.End()

	data, err := json.Marshal(dto.FromDomain(market))
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	start := time.Now()
	err = m.cacheStore.SetWithTTL(ctx, cacheKeyByID(market.ID), data, ttl)
	metrics.ObserveWithTrace(ctx,
		metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "set_by_id"),
		time.Since(start).Seconds(),
	)
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
