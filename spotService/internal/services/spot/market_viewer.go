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

var cacheRoleKeys = []string{roleAdminKey, roleViewerKey, roleUserKey}

type MarketRepository interface {
	GetMarketsPage(ctx context.Context, roleKey string, limit, offset uint64) ([]models.Market, error)
	GetMarketByID(ctx context.Context, id uuid.UUID) (models.Market, error)
}

type MarketCacheRepository interface {
	GetAll(ctx context.Context, roleKey string) ([]models.Market, error)
	SetAll(ctx context.Context, market []models.Market, roleKey string, ttl time.Duration) error
	DeleteAll(ctx context.Context, roleKey string) error
}

type MarketByIDCacheRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (models.Market, error)
	SetByID(ctx context.Context, market models.Market, ttl time.Duration) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
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

	roleKey, err := getRoleKeyFromContext(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return nil, 0, false, fmt.Errorf("%s: %w", op, err)
	}

	limit = normalizeLimit(limit, s.defaultLimit, s.maxLimit)

	if offset == 0 && limit <= s.cacheLimit {
		markets, nextOffset, hasMore, headErr := s.tryLoadHeadPage(ctx, roleKey, limit)
		if headErr == nil {
			span.SetAttributes(attributes.MarketsCountValue(len(markets)))
			return markets, nextOffset, hasMore, nil
		}

		tracing.RecordError(span, headErr)
		s.logger.Warn(ctx, "failed to load head page", zap.Error(headErr))
		return nil, 0, false, fmt.Errorf("%s: %w", op, headErr)
	}

	markets, pageErr := s.marketRepository.GetMarketsPage(ctx, roleKey, limit, offset)
	if pageErr != nil {
		if errors.Is(pageErr, repositoryErrors.ErrMarketStoreIsEmpty) {
			pageErr = serviceErrors.ErrMarketsNotFound
		}

		tracing.RecordError(span, pageErr)
		return nil, 0, false, fmt.Errorf("%s: %w", op, pageErr)
	}

	markets, nextOffset, hasMore := buildPageResponse(markets, limit, offset)
	span.SetAttributes(attributes.MarketsCountValue(len(markets)))

	return markets, nextOffset, hasMore, nil
}

func (s *MarketViewer) tryLoadHeadPage(
	ctx context.Context,
	roleKey string,
	limit uint64,
) ([]models.Market, uint64, bool, error) {
	allMarkets, err := s.marketCacheRepository.GetAll(ctx, roleKey)
	if err == nil {
		markets, nextOffset, hasMore := buildPageResponse(allMarkets, limit, 0)
		return markets, nextOffset, hasMore, nil
	}
	cacheError := err

	headMarkets, err := s.marketRepository.GetMarketsPage(ctx, roleKey, s.cacheLimit+1, 0)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
			return nil, 0, false, serviceErrors.ErrMarketsNotFound
		}

		if !errors.Is(cacheError, repositoryErrors.ErrMarketsNotFound) &&
			!errors.Is(cacheError, repositoryErrors.ErrMarketCacheCorrupted) {
			s.logger.Error(ctx, "head cache read failed", zap.Error(cacheError))
		}

		return nil, 0, false, err
	}

	markets, nextOffset, hasMore := buildPageResponse(headMarkets, limit, 0)

	if warmErr := s.warmHeadCache(ctx, roleKey, headMarkets); warmErr != nil {
		return markets, nextOffset, hasMore, nil
	}

	return markets, nextOffset, hasMore, nil
}

func (s *MarketViewer) warmHeadCache(
	ctx context.Context,
	roleKey string,
	markets []models.Market,
) error {
	if err := s.marketCacheRepository.SetAll(ctx, markets, roleKey, s.cacheTTL); err != nil {
		metrics.CacheWarmupsTotal.
			WithLabelValues(s.serviceName, "view_markets_lazy_warmup", roleKey, "error").
			Inc()

		s.logger.Warn(ctx, "failed to warm head cache",
			zap.String("role_key", roleKey),
			zap.Error(err),
		)
		return err
	}

	metrics.CacheWarmupsTotal.
		WithLabelValues(s.serviceName, "view_markets_lazy_warmup", roleKey, "success").
		Inc()

	return nil
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

	roleKey, err := getRoleKeyFromContext(ctx)
	if err != nil {
		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	market, err := s.getMarketActual(ctx, id)
	if err != nil {
		tracing.RecordError(span, err)
		return models.Market{}, err
	}

	if err = s.validateMarketAccess(roleKey, market, id); err != nil {
		tracing.RecordError(span, err)
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	return market, nil
}

func (s *MarketViewer) getMarketActual(
	ctx context.Context,
	id uuid.UUID,
) (models.Market, error) {
	const op = "MarketViewer.getMarketActual"

	market, err := s.marketByIDCacheRepository.GetByID(ctx, id)
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

func (s *MarketViewer) validateMarketAccess(
	roleKey string,
	market models.Market,
	id uuid.UUID,
) error {
	switch roleKey {
	case roleAdminKey:
		return nil

	case roleViewerKey:
		if market.DeletedAt != nil {
			return sharedErrors.ErrMarketNotFound{ID: id}
		}
		return nil

	case roleUserKey:
		if market.DeletedAt != nil {
			return sharedErrors.ErrMarketNotFound{ID: id}
		}
		if !market.Enabled {
			return serviceErrors.ErrDisabled{ID: id}
		}
		return nil

	default:
		return serviceErrors.ErrUserRoleNotSpecified
	}
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

	market, err := s.marketByIDCacheRepository.GetByID(ctx, id)
	if err == nil {
		return market, nil
	}
	if !errors.Is(err, repositoryErrors.ErrMarketNotFound) &&
		!errors.Is(err, repositoryErrors.ErrMarketCacheCorrupted) {
		s.logger.Error(ctx, "failed to re-check market by id cache", zap.Error(err))
	}

	market, err = s.marketRepository.GetMarketByID(ctx, id)
	if err != nil {
		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	if err = s.marketByIDCacheRepository.SetByID(ctx, market, s.cacheTTL); err != nil {
		return s.handleWarmupSetError(ctx, id, market, err, cleanupCorruptedCache), nil
	}

	metrics.CacheWarmupsTotal.WithLabelValues(s.serviceName, "load_and_warm_cache", "market_by_id", "success").Inc()
	return market, nil
}

func (s *MarketViewer) handleWarmupSetError(
	ctx context.Context,
	id uuid.UUID,
	market models.Market,
	cacheSetError error,
	cleanupCorruptedCache bool,
) models.Market {
	metrics.CacheWarmupsTotal.
		WithLabelValues(s.serviceName, "load_and_warm_cache", "market_by_id", "error").Inc()

	// При обычной ошибке записи возвращаем данные из repo без очистки ключа.
	// Если запрос пришёл после обнаружения corrupted cache, пытаемся удалить stale key.
	if !cleanupCorruptedCache {
		s.logger.Warn(ctx, "failed to update cache",
			zap.String("market_id", id.String()),
			zap.Error(cacheSetError),
		)
		return market
	}

	deleteErr := s.marketByIDCacheRepository.DeleteByID(ctx, id)
	if deleteErr != nil {
		metrics.CacheInvalidationsTotal.
			WithLabelValues(s.serviceName, "corrupted_cache_cleanup", "market_by_id", "error").Inc()

		s.logger.Error(ctx,
			"failed to update cache and failed to remove stale corrupted cache",
			zap.String("market_id", id.String()),
			zap.Error(cacheSetError),
			zap.NamedError("cleanup_error", deleteErr),
		)

		return market
	}

	metrics.CacheInvalidationsTotal.
		WithLabelValues(s.serviceName, "corrupted_cache_cleanup", "market_by_id", "success").Inc()

	s.logger.Warn(ctx,
		"failed to update cache, removed stale corrupted cache instead",
		zap.String("market_id", id.String()),
		zap.Error(cacheSetError),
	)

	return market
}

func (s *MarketViewer) RefreshAll(ctx context.Context) error {
	const op = "MarketViewer.RefreshAll"

	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.serviceTimeout)
	defer cancel()

	for _, roleKey := range cacheRoleKeys {
		markets, err := s.marketRepository.GetMarketsPage(refreshCtx, roleKey, s.cacheLimit+1, 0)
		if err != nil {
			if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
				return s.invalidateAllCache(refreshCtx)
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
func (s *MarketViewer) invalidateAllCache(ctx context.Context) error {
	const op = "MarketViewer.invalidateAllCache"

	for _, roleKey := range cacheRoleKeys {
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

func (s *MarketViewer) InvalidateByIDs(ctx context.Context, ids []uuid.UUID) error {
	const op = "MarketViewer.InvalidateByIDs"

	if len(ids) == 0 {
		return nil
	}

	seen := make(map[uuid.UUID]struct{}, len(ids))
	var invalidateErrs []error

	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		if err := s.marketByIDCacheRepository.DeleteByID(ctx, id); err != nil {
			metrics.CacheInvalidationsTotal.
				WithLabelValues(s.serviceName, "market_updated", "market_by_id", "error").
				Inc()

			invalidateErrs = append(invalidateErrs, fmt.Errorf("%s: delete market by id cache for market %s: %w", op, id.String(), err))
			continue
		}

		metrics.CacheInvalidationsTotal.
			WithLabelValues(s.serviceName, "market_updated", "market_by_id", "success").
			Inc()
	}

	if len(invalidateErrs) > 0 {
		return errors.Join(invalidateErrs...)
	}

	return nil
}

func buildPageResponse(markets []models.Market, limit, offset uint64) ([]models.Market, uint64, bool) {
	hasMore := len(markets) > int(limit)
	if hasMore {
		markets = markets[:limit]
	}

	var nextOffset uint64
	if hasMore {
		nextOffset = offset + limit
	}

	return markets, nextOffset, hasMore
}

func getRoleKeyFromContext(ctx context.Context) (string, error) {
	userRoles, ok := requestctx.UserRolesFromContext(ctx)
	if !ok {
		return "", serviceErrors.ErrUserRoleNotSpecified
	}

	roleKey, ok := effectiveUserRole(userRoles)
	if !ok {
		return "", serviceErrors.ErrUserRoleNotSpecified
	}

	return roleKey, nil
}

func effectiveUserRole(roles []models.UserRole) (string, bool) {
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

func normalizeLimit(limit, defaultLimit, maxLimit uint64) uint64 {
	if limit == 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}
