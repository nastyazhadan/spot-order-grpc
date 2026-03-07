package spot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
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
}

type MarketCacheRepository interface {
	GetAll(ctx context.Context, roleKey string) ([]models.Market, error)
	SetAll(ctx context.Context, market []models.Market, roleKey string, ttl time.Duration) error
}

type Service struct {
	marketRepository      MarketRepository
	marketCacheRepository MarketCacheRepository
	cacheTTL              time.Duration
	singleFlight          singleflight.Group
}

func NewService(repo MarketRepository, cacheRepo MarketCacheRepository, ttl time.Duration) *Service {
	return &Service{
		marketRepository:      repo,
		marketCacheRepository: cacheRepo,
		cacheTTL:              ttl,
	}
}

func (s *Service) ViewMarkets(ctx context.Context, userRoles []models.UserRole) ([]models.Market, error) {
	const op = "Service.ViewMarkets"

	roleKey, ok := effectiveUserRole(userRoles)
	if !ok {
		return nil, serviceErrors.ErrUserRoleNotSpecified
	}

	markets, err := s.getMarkets(ctx, roleKey)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return markets, nil
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

func (s *Service) getMarkets(ctx context.Context, roleKey string) ([]models.Market, error) {
	markets, err := s.marketCacheRepository.GetAll(ctx, roleKey)
	if err == nil {
		return markets, nil
	}

	if !errors.Is(err, repositoryErrors.ErrMarketCacheNotFound) {
		zapLogger.Error(ctx, "internal cache error", zap.Error(err))
	}

	return s.getMarketsWithSingleFlight(ctx, roleKey)
}

func (s *Service) getMarketsWithSingleFlight(ctx context.Context, roleKey string) ([]models.Market, error) {
	const op = "Service.getMarketsWithSingleFlight"

	resultKey := singleFlightKey + ":" + roleKey

	result, err, _ := s.singleFlight.Do(resultKey, func() (interface{}, error) {
		return s.loadAndWarmCache(ctx, roleKey)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return result.([]models.Market), nil
}

func (s *Service) loadAndWarmCache(ctx context.Context, roleKey string) ([]models.Market, error) {
	const op = "Service.loadAndWarmCache"

	if markets, err := s.marketCacheRepository.GetAll(ctx, roleKey); err == nil {
		return markets, nil
	}

	allMarkets, err := s.getMarketsFromRepo(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	filtered := filterByRole(allMarkets, roleKey)

	if err = s.marketCacheRepository.SetAll(ctx, filtered, roleKey, s.cacheTTL); err != nil {
		zapLogger.Warn(ctx, "failed to update cache", zap.Error(err))
	}

	return filtered, nil
}

func (s *Service) getMarketsFromRepo(ctx context.Context) ([]models.Market, error) {
	const op = "Service.getMarketsFromRepo"

	allMarkets, err := s.marketRepository.ListAll(ctx)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
			return nil, serviceErrors.ErrMarketsNotFound
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	sort.Slice(allMarkets, func(i, j int) bool {
		return allMarkets[i].Name < allMarkets[j].Name
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
