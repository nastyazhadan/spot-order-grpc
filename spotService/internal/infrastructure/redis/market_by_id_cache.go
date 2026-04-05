package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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

const cacheByIDKeyPrefix = "market:by_id"

type MarketByIDCacheRepository struct {
	cacheStore  *cache.Store
	serviceName string
}

func NewMarketByIDCacheRepository(store *cache.Store, serviceName string) *MarketByIDCacheRepository {
	return &MarketByIDCacheRepository{
		cacheStore:  store,
		serviceName: serviceName,
	}
}

func (m *MarketByIDCacheRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (models.Market, error) {
	const op = "redis.MarketByIDCacheRepository.GetByID"

	ctx, span := tracing.StartSpan(ctx, "redis.get_market_by_id",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attributes.DBSystemValue(dbSystem),
			attributes.MarketIDValue(id.String()),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(ctx,
			metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "get_by_id"),
			time.Since(start).Seconds(),
		)
	}()

	data, err := m.cacheStore.Get(ctx, cacheByIDKey(id))
	if err != nil {
		if errors.Is(err, sharedErrors.ErrCacheNotFound) {
			metrics.CacheMissesTotal.WithLabelValues(m.serviceName, "get_by_id").Inc()
			return models.Market{}, repositoryErrors.ErrMarketNotFound
		}

		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	market, reason, err := decodeCachedMarket(data)
	if err != nil {
		m.invalidateCorruptedCache(ctx, span, id, reason, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, repositoryErrors.ErrMarketCacheCorrupted)
	}

	metrics.CacheHitsTotal.WithLabelValues(m.serviceName, "get_by_id").Inc()
	return market, nil
}

func (m *MarketByIDCacheRepository) invalidateCorruptedCache(
	ctx context.Context,
	span trace.Span,
	id uuid.UUID,
	reason string,
	cause error,
) {
	span.SetAttributes(
		attributes.CacheCorruptedValue(true),
		attributes.CacheCorruptedReasonValue(reason),
		attributes.MarketIDValue(id.String()),
	)
	tracing.RecordError(span, cause)

	if err := m.DeleteByID(ctx, id); err != nil {
		span.SetAttributes(attributes.CacheInvalidationFailedValue(true))
		tracing.RecordError(span, err)

		metrics.CacheInvalidationsTotal.
			WithLabelValues(m.serviceName, reason, "market_by_id", "error").Inc()
		return
	}

	metrics.CacheInvalidationsTotal.
		WithLabelValues(m.serviceName, reason, "market_by_id", "success").Inc()
}

func (m *MarketByIDCacheRepository) SetByID(
	ctx context.Context,
	market models.Market,
	ttl time.Duration,
) error {
	const op = "redis.MarketByIDCacheRepository.SetByID"

	ctx, span := tracing.StartSpan(ctx, "redis.set_market_by_id",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attributes.DBSystemValue(dbSystem),
			attributes.MarketIDValue(market.ID.String()),
			attributes.CacheTTLValue(ttl),
		),
	)
	defer span.End()

	data, err := encodeMarket(market)
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	start := time.Now()
	err = m.cacheStore.SetWithTTL(ctx, cacheByIDKey(market.ID), data, ttl)
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

func (m *MarketByIDCacheRepository) DeleteByID(
	ctx context.Context,
	id uuid.UUID,
) error {
	const op = "redis.MarketByIDCacheRepository.DeleteByID"

	ctx, span := tracing.StartSpan(ctx, "redis.delete_market_by_id",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attributes.DBSystemValue(dbSystem),
			attributes.MarketIDValue(id.String()),
		),
	)
	defer span.End()

	start := time.Now()
	err := m.cacheStore.Delete(ctx, cacheByIDKey(id))
	metrics.ObserveWithTrace(ctx,
		metrics.CacheOperationDuration.WithLabelValues(m.serviceName, "delete_by_id"),
		time.Since(start).Seconds(),
	)
	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func decodeCachedMarket(data []byte) (models.Market, string, error) {
	var redisView dto.MarketRedisView
	if err := json.Unmarshal(data, &redisView); err != nil {
		return models.Market{}, "json_unmarshal", err
	}

	market, err := redisView.ToDomain()
	if err != nil {
		return models.Market{}, "dto_to_domain", err
	}

	return market, "", nil
}

func encodeMarket(market models.Market) ([]byte, error) {
	redisView := dto.FromDomain(market)

	data, err := json.Marshal(redisView)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func cacheByIDKey(id uuid.UUID) string {
	return fmt.Sprintf("%s:%s", cacheByIDKeyPrefix, id.String())
}
