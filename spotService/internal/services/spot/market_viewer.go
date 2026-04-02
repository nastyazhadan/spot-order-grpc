package spot

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
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
	singleFlightKey = "load_markets"
	roleAdminKey    = "admin"
	roleViewerKey   = "viewer"
	roleUserKey     = "user"
)

type MarketRepository interface {
	ListAll(ctx context.Context) ([]models.Market, error)
	GetByID(ctx context.Context, id uuid.UUID) (models.Market, error)
}

type MarketCacheRepository interface {
	GetAll(ctx context.Context, roleKey string) ([]models.Market, error)
	SetAll(ctx context.Context, market []models.Market, roleKey string, ttl time.Duration) error
	Delete(ctx context.Context, roleKey string) error
}

type MarketViewer struct {
	marketRepository      MarketRepository
	marketCacheRepository MarketCacheRepository
	cacheTTL              time.Duration
	serviceTimeout        time.Duration
	defaultLimit          uint64
	maxLimit              uint64
	serviceName           string
	singleFlight          singleflight.Group
	logger                *zapLogger.Logger
}

func NewMarketViewer(
	repo MarketRepository,
	cacheRepo MarketCacheRepository,
	ttl, timeout time.Duration,
	defaultLimit, maxLimit uint64,
	serviceName string,
	logger *zapLogger.Logger,
) *MarketViewer {
	return &MarketViewer{
		marketRepository:      repo,
		marketCacheRepository: cacheRepo,
		cacheTTL:              ttl,
		serviceTimeout:        timeout,
		defaultLimit:          defaultLimit,
		maxLimit:              maxLimit,
		serviceName:           serviceName,
		logger:                logger,
	}
}

func (s *MarketViewer) ViewMarkets(ctx context.Context, limit, offset uint64) ([]models.Market, uint64, bool, error) {
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

	markets, err := s.getMarkets(ctx, roleKey)
	if err != nil {
		tracing.RecordError(span, err)
		return nil, 0, false, fmt.Errorf("%s: %w", op, err)
	}

	page, nextOffset, hasMore := paginateMarkets(markets, limit, offset)
	span.SetAttributes(attributes.MarketsCountValue(len(page)))

	return page, nextOffset, hasMore, nil
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

func (s *MarketViewer) getMarketActual(ctx context.Context, id uuid.UUID) (models.Market, error) {
	const op = "MarketViewer.getMarketActual"

	market, err := s.marketRepository.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketNotFound) {
			return models.Market{}, sharedErrors.ErrMarketNotFound{ID: id}
		}

		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	return market, nil
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

func canViewMarketByRoleKey(market models.Market, roleKey string) bool {
	switch roleKey {
	case roleAdminKey:
		// Админ видит все рынки (включая disabled и deleted)
		return true
	case roleViewerKey:
		// Viewer видит все неудаленные рынки (включая disabled)
		return market.DeletedAt == nil
	default:
		// User видит только enabled и неудаленные рынки
		return market.DeletedAt == nil && market.Enabled
	}
}

func (s *MarketViewer) getMarkets(ctx context.Context, roleKey string) ([]models.Market, error) {
	markets, err := s.marketCacheRepository.GetAll(ctx, roleKey)
	if err == nil {
		return markets, nil
	}

	if errors.Is(err, repositoryErrors.ErrMarketsNotFound) {
		return s.getMarketsWithSingleFlight(ctx, roleKey)
	}

	s.logger.Error(ctx, "internal cache error", zap.Error(err))
	metrics.CacheFallbacksTotal.
		WithLabelValues(s.serviceName, "get_all", "internal_cache_error").
		Inc()

	return s.getMarketsWithSingleFlight(ctx, roleKey)
}

func (s *MarketViewer) getMarketsWithSingleFlight(ctx context.Context, roleKey string) ([]models.Market, error) {
	const op = "MarketViewer.getMarketsWithSingleFlight"

	resultKey := singleFlightKey + ":" + roleKey

	result, err, _ := s.singleFlight.Do(resultKey, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.serviceTimeout)
		defer cancel()

		return s.loadAndWarmCache(loadCtx, roleKey)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	markets, ok := result.([]models.Market)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected result type %T", op, result)
	}

	return markets, nil
}

func (s *MarketViewer) loadAndWarmCache(ctx context.Context, roleKey string) ([]models.Market, error) {
	const op = "MarketViewer.loadAndWarmCache"

	ctx, span := tracing.StartSpan(ctx, "spot.load_and_warm_cache",
		trace.WithAttributes(attributes.UserRoleKeyValue(roleKey)),
	)
	defer span.End()

	allMarkets, err := s.getMarketsFromRepo(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	filtered := filterByRole(allMarkets, roleKey)

	if err = s.marketCacheRepository.SetAll(ctx, filtered, roleKey, s.cacheTTL); err != nil {
		// При ошибке записи старые ключи не удаляются, чтобы не возникало лишних промахов кэша
		metrics.CacheWarmupsTotal.
			WithLabelValues(s.serviceName, "load_and_warm_cache", roleKey, "error").
			Inc()

		s.logger.Warn(ctx, "failed to update cache", zap.Error(err))
		return filtered, nil
	}
	metrics.CacheWarmupsTotal.
		WithLabelValues(s.serviceName, "load_and_warm_cache", roleKey, "success").
		Inc()

	return filtered, nil
}

func (s *MarketViewer) getMarketsFromRepo(ctx context.Context) ([]models.Market, error) {
	const op = "MarketViewer.getMarketsFromRepo"

	ctx, span := tracing.StartSpan(ctx, "spot.get_markets_from_repo")
	defer span.End()

	allMarkets, err := s.marketRepository.ListAll(ctx)
	if err != nil {
		tracing.RecordError(span, err)

		if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
			return nil, serviceErrors.ErrMarketsNotFound
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	slices.SortFunc(allMarkets, func(a, b models.Market) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return allMarkets, nil
}

func filterByRole(markets []models.Market, roleKey string) []models.Market {
	out := make([]models.Market, 0, len(markets))

	for _, market := range markets {
		if canViewMarketByRoleKey(market, roleKey) {
			out = append(out, market)
		}
	}

	return out
}

func (s *MarketViewer) RefreshAll(ctx context.Context) error {
	const op = "MarketViewer.RefreshAll"

	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.serviceTimeout)
	defer cancel()

	allMarkets, err := s.marketRepository.ListAll(refreshCtx)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
			return s.invalidateAll(refreshCtx)
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	slices.SortFunc(allMarkets, func(a, b models.Market) int {
		return cmp.Compare(a.Name, b.Name)
	})

	for _, roleKey := range []string{roleAdminKey, roleViewerKey, roleUserKey} {
		filtered := filterByRole(allMarkets, roleKey)

		if err = s.marketCacheRepository.SetAll(refreshCtx, filtered, roleKey, s.cacheTTL); err != nil {
			// При ошибке записи старые ключи не удаляются, чтобы не возникало лишних промахов кэша
			metrics.CacheWarmupsTotal.
				WithLabelValues(s.serviceName, "refresh_all", roleKey, "error").
				Inc()

			return fmt.Errorf("%s: set cache for role %s: %w", op, roleKey, err)
		}

		metrics.CacheWarmupsTotal.
			WithLabelValues(s.serviceName, "refresh_all", roleKey, "success").
			Inc()
	}

	return nil
}

// Удаление всех ключей при пустом market store
func (s *MarketViewer) invalidateAll(ctx context.Context) error {
	const op = "MarketViewer.invalidateAll"

	for _, roleKey := range []string{roleAdminKey, roleViewerKey, roleUserKey} {
		if err := s.marketCacheRepository.Delete(ctx, roleKey); err != nil {
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

func paginateMarkets(markets []models.Market, limit, offset uint64) ([]models.Market, uint64, bool) {
	if limit <= 0 {
		return []models.Market{}, 0, false
	}

	if len(markets) == 0 || offset >= uint64(len(markets)) {
		return []models.Market{}, 0, false
	}

	start := int(offset)
	end := start + int(limit)
	if end > len(markets) {
		end = len(markets)
	}

	hasMore := end < len(markets)
	if !hasMore {
		return markets[start:end], 0, false
	}

	return markets[start:end], uint64(end), true
}
