package spot

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
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
	GetByID(ctx context.Context, id uuid.UUID) (models.Market, error)
	SetByID(ctx context.Context, market models.Market, ttl time.Duration) error
}

type MarketViewer struct {
	marketRepository      MarketRepository
	marketCacheRepository MarketCacheRepository
	cacheTTL              time.Duration
	serviceTimeout        time.Duration
	singleFlight          singleflight.Group
	logger                *zapLogger.Logger
}

func NewMarketViewer(
	repo MarketRepository,
	cacheRepo MarketCacheRepository,
	ttl, timeout time.Duration,
	logger *zapLogger.Logger,
) *MarketViewer {
	return &MarketViewer{
		marketRepository:      repo,
		marketCacheRepository: cacheRepo,
		cacheTTL:              ttl,
		serviceTimeout:        timeout,
		logger:                logger,
	}
}

func (s *MarketViewer) ViewMarkets(ctx context.Context, userRoles []models.UserRole) ([]models.Market, error) {
	const op = "MarketViewer.ViewMarkets"

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.serviceTimeout)
		defer cancel()
	}

	ctx, span := tracing.StartSpan(ctx, "spot.view_markets")
	defer span.End()

	roleKey, ok := effectiveUserRole(userRoles)
	if !ok {
		err := serviceErrors.ErrUserRoleNotSpecified
		span.RecordError(err)
		return nil, err
	}

	markets, err := s.getMarkets(ctx, roleKey)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return markets, nil
}

func (s *MarketViewer) GetMarketByID(ctx context.Context, id uuid.UUID) (models.Market, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.serviceTimeout)
		defer cancel()
	}

	ctx, span := tracing.StartSpan(ctx, "spot.get_market_by_id",
		trace.WithAttributes(attribute.String("market_id", id.String())),
	)
	defer span.End()

	market, err := s.getMarketByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return models.Market{}, err
	}

	return market, nil
}

func (s *MarketViewer) getMarketByID(ctx context.Context, id uuid.UUID) (models.Market, error) {
	const op = "MarketViewer.getMarketByID"

	market, err := s.marketCacheRepository.GetByID(ctx, id)
	if err == nil {
		return market, nil
	}

	if !errors.Is(err, repositoryErrors.ErrMarketNotFound) {
		s.logger.Error(ctx, "internal cache error", zap.Error(err))
	}

	market, err = s.marketRepository.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketNotFound) {
			return models.Market{}, serviceErrors.ErrMarketNotFound
		}

		return models.Market{}, fmt.Errorf("%s: %w", op, err)
	}

	if err = s.marketCacheRepository.SetByID(ctx, market, s.cacheTTL); err != nil {
		s.logger.Warn(ctx, "failed to cache market by id", zap.Error(err))
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

func (s *MarketViewer) getMarkets(ctx context.Context, roleKey string) ([]models.Market, error) {
	markets, err := s.marketCacheRepository.GetAll(ctx, roleKey)
	if err == nil {
		return markets, nil
	}

	if !errors.Is(err, repositoryErrors.ErrMarketsNotFound) {
		s.logger.Error(ctx, "internal cache error", zap.Error(err))
	}

	return s.getMarketsWithSingleFlight(ctx, roleKey)
}

func (s *MarketViewer) getMarketsWithSingleFlight(ctx context.Context, roleKey string) ([]models.Market, error) {
	const op = "MarketViewer.getMarketsWithSingleFlight"

	resultKey := singleFlightKey + ":" + roleKey

	result, err, _ := s.singleFlight.Do(resultKey, func() (interface{}, error) {
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
		trace.WithAttributes(attribute.String("roleKey", roleKey)),
	)
	defer span.End()

	if markets, err := s.marketCacheRepository.GetAll(ctx, roleKey); err == nil {
		return markets, nil
	}

	allMarkets, err := s.getMarketsFromRepo(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	filtered := filterByRole(allMarkets, roleKey)

	if err = s.marketCacheRepository.SetAll(ctx, filtered, roleKey, s.cacheTTL); err != nil {
		span.RecordError(err)
		s.logger.Warn(ctx, "failed to update cache", zap.Error(err))
	}

	for _, market := range allMarkets {
		if err = s.marketCacheRepository.SetByID(ctx, market, s.cacheTTL); err != nil {
			span.RecordError(err)
			s.logger.Warn(ctx, "failed cache market by id", zap.Error(err),
				zap.String("market_id", market.ID.String()))
		}
	}

	return filtered, nil
}

func (s *MarketViewer) getMarketsFromRepo(ctx context.Context) ([]models.Market, error) {
	const op = "MarketViewer.getMarketsFromRepo"

	ctx, span := tracing.StartSpan(ctx, "spot.get_markets_from_repo")
	defer span.End()

	allMarkets, err := s.marketRepository.ListAll(ctx)
	if err != nil {
		span.RecordError(err)
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
	switch roleKey {
	case roleAdminKey:
		// Админ видит все рынки (включая disabled и deleted)
		return markets
	case roleViewerKey:
		// Viewer видит все неудаленные рынки (включая disabled)
		out := make([]models.Market, 0, len(markets))
		for _, market := range markets {
			if market.DeletedAt == nil {
				out = append(out, market)
			}
		}
		return out
	default:
		// User видит только enabled и неудаленные рынки
		out := make([]models.Market, 0, len(markets))
		for _, market := range markets {
			if market.DeletedAt == nil && market.Enabled {
				out = append(out, market)
			}
		}
		return out
	}
}
