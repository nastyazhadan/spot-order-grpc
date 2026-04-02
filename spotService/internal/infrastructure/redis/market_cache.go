package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/redis"
)

const (
	cacheKeyPrefix = "market:cache"
	dbSystem       = "redis"
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

func (m *MarketCacheRepository) GetAll(
	ctx context.Context,
	roleKey string,
) ([]models.Market, error) {
	const op = "redis.MarketCacheRepository.GetAll"

	ctx, span := tracing.StartSpan(ctx, "redis.get_markets",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attributes.DBSystemValue(dbSystem),
			attributes.UserRoleKeyValue(roleKey),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(
			ctx,
			metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "get_all"),
			time.Since(start).Seconds(),
		)
	}()

	data, err := m.cacheStore.Get(ctx, cacheKey(roleKey))
	if err != nil {
		if errors.Is(err, sharedErrors.ErrCacheNotFound) {
			metrics.CacheMissesTotal.WithLabelValues(m.serviceName, "get_all").Inc()
			return nil, repositoryErrors.ErrMarketsNotFound
		}

		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var redisViews []dto.MarketRedisView
	if err = json.Unmarshal(data, &redisViews); err != nil {
		m.invalidateCorruptedCache(ctx, span, roleKey, "json_unmarshal", err)

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	markets := make([]models.Market, 0, len(redisViews))
	for _, redisView := range redisViews {
		market, mapError := redisView.ToDomain()
		if mapError != nil {
			m.invalidateCorruptedCache(ctx, span, roleKey, "dto_to_domain", mapError)

			return nil, fmt.Errorf("%s: %w", op, mapError)
		}
		markets = append(markets, market)
	}

	metrics.CacheHitsTotal.WithLabelValues(m.serviceName, "get_all").Inc()
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
			attributes.DBSystemValue(dbSystem),
			attributes.UserRoleKeyValue(roleKey),
			attributes.MarketsCountValue(len(markets)),
			attributes.CacheTTLValue(ttl),
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

func (m *MarketCacheRepository) Delete(ctx context.Context, roleKey string) error {
	const op = "redis.MarketCacheRepository.Delete"

	start := time.Now()
	err := m.cacheStore.Delete(ctx, cacheKey(roleKey))
	metrics.ObserveWithTrace(ctx,
		metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "delete"),
		time.Since(start).Seconds(),
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// Удаление ключей, если возникла ошибка
func (m *MarketCacheRepository) invalidateCorruptedCache(
	ctx context.Context,
	span trace.Span,
	roleKey string,
	reason string,
	cause error,
) {
	span.SetAttributes(
		attributes.CacheCorruptedValue(true),
		attributes.CacheCorruptedReasonValue(reason),
	)
	tracing.RecordError(span, cause)

	if err := m.Delete(ctx, roleKey); err != nil {
		span.SetAttributes(attributes.CacheInvalidationFailedValue(true))
		tracing.RecordError(span, err)

		metrics.CacheInvalidationsTotal.
			WithLabelValues(m.serviceName, reason, roleKey, "error").
			Inc()
		return
	}

	metrics.CacheInvalidationsTotal.
		WithLabelValues(m.serviceName, reason, roleKey, "success").
		Inc()
}

func cacheKey(roleKey string) string {
	return fmt.Sprintf("%s:%s", cacheKeyPrefix, roleKey)
}
