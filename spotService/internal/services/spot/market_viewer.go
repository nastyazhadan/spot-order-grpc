package spot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"
)

const (
	singleFlightKeyPrefix = "market_by_id:"
	roleAdminKey          = "admin"
	roleViewerKey         = "viewer"
	roleUserKey           = "user"
)

type MarketRepository interface {
	ListAll(ctx context.Context) ([]models.Market, error)
	ListPage(ctx context.Context, roleKey string, limit, offset uint64) ([]models.Market, error)
	GetByID(ctx context.Context, id uuid.UUID) (models.Market, error)
}

type MarketCacheRepository interface {
	GetAll(ctx context.Context, roleKey string) ([]models.Market, error)
	SetAll(ctx context.Context, market []models.Market, roleKey string, ttl time.Duration) error
	DeleteAll(ctx context.Context, roleKey string) error
}

type MarketByIDCacheRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.Market, error)
	Set(ctx context.Context, market models.Market, ttl time.Duration) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type MarketViewer struct {
	marketRepository          MarketRepository
	marketCacheRepository     MarketCacheRepository
	marketByIDCacheRepository MarketByIDCacheRepository
	cacheTTL                  time.Duration
	serviceTimeout            time.Duration
	defaultLimit              uint64
	maxLimit                  uint64
	cacheLimit                uint64
	serviceName               string
	singleFlight              singleflight.Group
	logger                    *zapLogger.Logger
}

func NewMarketViewer(
	repo MarketRepository,
	cacheRepo MarketCacheRepository,
	byIDCacheRepo MarketByIDCacheRepository,
	ttl, timeout time.Duration,
	defaultLimit, maxLimit, cacheLimit uint64,
	serviceName string,
	logger *zapLogger.Logger,
) *MarketViewer {
	return &MarketViewer{
		marketRepository:          repo,
		marketCacheRepository:     cacheRepo,
		marketByIDCacheRepository: byIDCacheRepo,
		cacheTTL:                  ttl,
		serviceTimeout:            timeout,
		defaultLimit:              defaultLimit,
		maxLimit:                  maxLimit,
		cacheLimit:                cacheLimit,
		serviceName:               serviceName,
		logger:                    logger,
	}
}

func (s *MarketViewer) ViewMarkets(
	ctx context.Context,
	limit, offset uint64,
) ([]models.Market, uint64, bool, error) {
	const op = "MarketViewer.ViewMarkets"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.serviceTimeout)
		defer cancel()
	}

	ctx, span := tracing.StartSpan(ctx, "spot.view_markets")
	defer span.End()

	userRoles, ok := requestctx.UserRolesFromContext(ctx)
	if !ok {
		err := serviceErrors.ErrUserRoleNotSpecified
		tracing.RecordError(span, err)
		return nil, 0, false, err
	}

	roleKey, ok := effectiveUserRole(userRoles)
	if !ok {
		err := serviceErrors.ErrUserRoleNotSpecified
		tracing.RecordError(span, err)
		return nil, 0, false, fmt.Errorf("%s: %w", op, err)
	}

	if limit == 0 {
		limit = s.defaultLimit
	}
	if limit > s.maxLimit {
		limit = s.maxLimit
	}

	if offset == 0 && limit <= s.cacheLimit {
		markets, nextOffset, hasMore, err := s.viewMarketsFromHeadCache(ctx, roleKey, limit)
		if err == nil {
			span.SetAttributes(attributes.MarketsCountValue(len(markets)))
			return markets, nextOffset, hasMore, nil
		}

		headMarkets, headErr := s.loadHeadMarketsAndWarmCache(ctx, roleKey)
		if headErr == nil {
			markets, nextOffset, hasMore = s.buildHeadPageResponse(headMarkets, limit)
			span.SetAttributes(attributes.MarketsCountValue(len(markets)))
			return markets, nextOffset, hasMore, nil
		}

		if !errors.Is(err, repositoryErrors.ErrMarketsNotFound) &&
			!errors.Is(err, repositoryErrors.ErrMarketCacheCorrupted) {
			s.logger.Error(ctx, "head cache read failed", zap.Error(err))
		}

		s.logger.Warn(ctx, "failed to warm head cache from db, falling back to direct page query", zap.Error(headErr))
	}

	markets, err := s.marketRepository.ListPage(ctx, roleKey, limit, offset)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
			err = serviceErrors.ErrMarketsNotFound
		}

		tracing.RecordError(span, err)
		return nil, 0, false, fmt.Errorf("%s: %w", op, err)
	}

	hasMore := len(markets) > int(limit)
	if hasMore {
		markets = markets[:limit]
	}

	var nextOffset uint64
	if hasMore {
		nextOffset = offset + limit
	}

	span.SetAttributes(attributes.MarketsCountValue(len(markets)))

	return markets, nextOffset, hasMore, nil
}

func (s *MarketViewer) GetMarketByID(
	ctx context.Context,
	id uuid.UUID,
) (models.Market, error) {
	const op = "MarketViewer.GetMarketByID"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.serviceTimeout)
		defer cancel()
	}

	ctx, span := tracing.StartSpan(ctx, "spot.get_market_by_id",
		trace.WithAttributes(attributes.MarketIDValue(id.String())),
	)
	defer span.End()

	userRoles, ok := requestctx.UserRolesFromContext(ctx)
	if !ok {
		err := serviceErrors.ErrUserRoleNotSpecified
		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	roleKey, ok := effectiveUserRole(userRoles)
	if !ok {
		err := serviceErrors.ErrUserRoleNotSpecified
		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	market, err := s.getMarketActual(ctx, id)
	if err != nil {
		tracing.RecordError(span, err)
		return models.Market{}, err
	}

	switch roleKey {
	case roleAdminKey:
		return market, nil

	case roleViewerKey:
		if market.DeletedAt != nil {
			err = sharedErrors.ErrMarketNotFound{ID: id}
			tracing.RecordError(span, err)
			return models.Market{}, fmt.Errorf("%s: %w", op, err)
		}
		return market, nil

	default:
		if market.DeletedAt != nil {
			err = sharedErrors.ErrMarketNotFound{ID: id}
			tracing.RecordError(span, err)
			return models.Market{}, fmt.Errorf("%s: %w", op, err)
		}
		if !market.Enabled {
			err = serviceErrors.ErrDisabled{ID: id}
			tracing.RecordError(span, err)
			return models.Market{}, fmt.Errorf("%s: %w", op, err)
		}
		return market, nil
	}
}

func (s *MarketViewer) getMarketActual(
	ctx context.Context,
	id uuid.UUID,
) (models.Market, error) {
	const op = "MarketViewer.getMarketActual"

	market, err := s.marketByIDCacheRepository.Get(ctx, id)
	if err == nil {
		return market, nil
	}

	cleanupCorruptedCache := false
	if errors.Is(err, repositoryErrors.ErrMarketCacheCorrupted) {
		cleanupCorruptedCache = true
	} else if !errors.Is(err, repositoryErrors.ErrMarketNotFound) {
		s.logger.Error(ctx, "failed to read market by id from cache", zap.Error(err))
	}

	market, err = s.getMarketWithSingleFlight(ctx, id, cleanupCorruptedCache)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketNotFound) {
			return models.Market{}, sharedErrors.ErrMarketNotFound{ID: id}
		}

		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	return market, nil
}

func effectiveUserRole(
	roles []models.UserRole,
) (string, bool) {
	resultRole := ""

	for _, role := range roles {
		switch role {
		case models.UserRoleAdmin:
			return roleAdminKey, true
		case models.UserRoleViewer:
			resultRole = roleViewerKey
		case models.UserRoleUser:
			if resultRole == "" {
				resultRole = roleUserKey
			}
		default:
		}
	}
	return resultRole, resultRole != ""
}

func (s *MarketViewer) viewMarketsFromHeadCache(
	ctx context.Context,
	roleKey string,
	limit uint64,
) ([]models.Market, uint64, bool, error) {
	markets, err := s.marketCacheRepository.GetAll(ctx, roleKey)
	if err != nil {
		return nil, 0, false, err
	}

	hasMore := len(markets) > int(limit)
	if hasMore {
		markets = markets[:limit]
	}

	var nextOffset uint64
	if hasMore {
		nextOffset = limit
	}

	return markets, nextOffset, hasMore, nil
}

func (s *MarketViewer) getMarketWithSingleFlight(
	ctx context.Context,
	id uuid.UUID,
	cleanupCorruptedCache bool,
) (models.Market, error) {
	const op = "MarketViewer.getMarketWithSingleFlight"

	resultKey := singleFlightKeyPrefix + id.String()

	result, err, _ := s.singleFlight.Do(resultKey, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.serviceTimeout)
		defer cancel()

		return s.loadMarketAndWarmCache(loadCtx, id, cleanupCorruptedCache)
	})
	if err != nil {
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	market, ok := result.(models.Market)
	if !ok {
		return models.Market{}, fmt.Errorf("%s: unexpected result type %T", op, result)
	}

	return market, nil
}

func (s *MarketViewer) loadMarketAndWarmCache(
	ctx context.Context,
	id uuid.UUID,
	cleanupCorruptedCache bool,
) (models.Market, error) {
	const op = "MarketViewer.loadMarketAndWarmCache"

	ctx, span := tracing.StartSpan(ctx, "spot.load_market_and_warm_cache",
		trace.WithAttributes(attributes.MarketIDValue(id.String())),
	)
	defer span.End()

	market, err := s.marketByIDCacheRepository.Get(ctx, id)
	if err == nil {
		return market, nil
	}

	if !errors.Is(err, repositoryErrors.ErrMarketNotFound) &&
		!errors.Is(err, repositoryErrors.ErrMarketCacheCorrupted) {
		s.logger.Error(ctx, "failed to re-check market by id cache", zap.Error(err))
	}

	market, err = s.marketRepository.GetByID(ctx, id)
	if err != nil {
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	if err = s.marketByIDCacheRepository.Set(ctx, market, s.cacheTTL); err != nil {
		// При обычной ошибке записи возвращаем данные из repo без очистки ключа.
		// Если запрос пришёл после обнаружения corrupted cache, пытаемся удалить stale key.
		metrics.CacheWarmupsTotal.WithLabelValues(s.serviceName, "load_and_warm_cache", "market_by_id", "error").Inc()

		if cleanupCorruptedCache {
			deleteErr := s.marketByIDCacheRepository.Delete(ctx, id)
			if deleteErr != nil {
				metrics.CacheInvalidationsTotal.
					WithLabelValues(s.serviceName, "corrupted_cache_cleanup", "market_by_id", "error").Inc()

				s.logger.Error(ctx,
					"failed to update cache and failed to remove stale corrupted cache",
					zap.String("market_id", id.String()),
					zap.Error(err),
					zap.NamedError("cleanup_error", deleteErr),
				)
			} else {
				metrics.CacheInvalidationsTotal.
					WithLabelValues(s.serviceName, "corrupted_cache_cleanup", "market_by_id", "success").Inc()

				s.logger.Warn(ctx,
					"failed to update cache, removed stale corrupted cache instead",
					zap.String("market_id", id.String()),
					zap.Error(err),
				)
			}
			return market, nil
		}

		s.logger.Warn(ctx, "failed to update cache", zap.String("market_id", id.String()), zap.Error(err))
		return market, nil
	}

	metrics.CacheWarmupsTotal.WithLabelValues(s.serviceName, "load_and_warm_cache", "market_by_id", "success").Inc()
	return market, nil
}

func (s *MarketViewer) loadHeadMarketsAndWarmCache(
	ctx context.Context,
	roleKey string,
) ([]models.Market, error) {
	const op = "MarketViewer.loadHeadMarketsAndWarmCache"

	markets, err := s.marketRepository.ListPage(ctx, roleKey, s.cacheLimit+1, 0)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
			return nil, serviceErrors.ErrMarketsNotFound
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = s.marketCacheRepository.SetAll(ctx, markets, roleKey, s.cacheTTL); err != nil {
		s.logger.Warn(ctx, "failed to warm head cache", zap.String("role_key", roleKey), zap.Error(err))
		return markets, nil
	}

	metrics.CacheWarmupsTotal.
		WithLabelValues(s.serviceName, "view_markets_lazy_warmup", roleKey, "success").
		Inc()

	return markets, nil
}

func (s *MarketViewer) RefreshAll(
	ctx context.Context,
) error {
	const op = "MarketViewer.RefreshAll"

	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.serviceTimeout)
	defer cancel()

	for _, roleKey := range []string{roleAdminKey, roleViewerKey, roleUserKey} {
		markets, err := s.marketRepository.ListPage(refreshCtx, roleKey, s.cacheLimit+1, 0)
		if err != nil {
			if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
				return s.invalidateAll(refreshCtx)
			}

			metrics.CacheWarmupsTotal.
				WithLabelValues(s.serviceName, "refresh_all", roleKey, "error").Inc()

			return fmt.Errorf("%s: load head cache for role %s: %w", op, roleKey, err)
		}

		if err = s.marketCacheRepository.SetAll(refreshCtx, markets, roleKey, s.cacheTTL); err != nil {
			metrics.CacheWarmupsTotal.
				WithLabelValues(s.serviceName, "refresh_all", roleKey, "error").Inc()

			return fmt.Errorf("%s: set cache for role %s: %w", op, roleKey, err)
		}

		metrics.CacheWarmupsTotal.
			WithLabelValues(s.serviceName, "refresh_all", roleKey, "success").Inc()
	}

	return nil
}

// Удаление всех ключей при пустом market store
func (s *MarketViewer) invalidateAll(
	ctx context.Context,
) error {
	const op = "MarketViewer.invalidateAll"

	for _, roleKey := range []string{roleAdminKey, roleViewerKey, roleUserKey} {
		if err := s.marketCacheRepository.DeleteAll(ctx, roleKey); err != nil {
			metrics.CacheInvalidationsTotal.
				WithLabelValues(s.serviceName, "refresh_empty_store", roleKey, "error").
				Inc()

			return fmt.Errorf("%s: delete cache for role %s: %w", op, roleKey, err)
		}

		metrics.CacheInvalidationsTotal.
			WithLabelValues(s.serviceName, "refresh_empty_store", roleKey, "success").
			Inc()
	}

	return nil
}

func (s *MarketViewer) buildHeadPageResponse(
	markets []models.Market,
	limit uint64,
) ([]models.Market, uint64, bool) {
	hasMore := len(markets) > int(limit)
	if hasMore {
		markets = markets[:limit]
	}

	var nextOffset uint64
	if hasMore {
		nextOffset = limit
	}

	return markets, nextOffset, hasMore
}
