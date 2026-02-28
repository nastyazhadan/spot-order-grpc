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

type MarketRepository interface {
	ListAll(ctx context.Context) ([]models.Market, error)
}

type MarketCacheRepository interface {
	GetAll(ctx context.Context) ([]models.Market, error)
	SetAll(ctx context.Context, market []models.Market, ttl time.Duration) error
}

type Service struct {
	marketRepository      MarketRepository
	marketCacheRepository MarketCacheRepository
	cacheTTL              time.Duration

	singleFlight singleflight.Group
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

	markets, err := s.getMarkets(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return s.filterByRoles(markets, userRoles), nil
}

func (s *Service) getMarkets(ctx context.Context) ([]models.Market, error) {
	markets, err := s.getMarketsFromCache(ctx)
	if err == nil {
		return markets, nil
	}

	if !errors.Is(err, repositoryErrors.ErrMarketCacheNotFound) {
		zapLogger.Error(ctx, "internal cache error", zap.Error(err))
	}

	return s.getMarketsWithSingleFlight(ctx)
}

func (s *Service) getMarketsFromCache(ctx context.Context) ([]models.Market, error) {
	return s.marketCacheRepository.GetAll(ctx)
}

func (s *Service) getMarketsWithSingleFlight(ctx context.Context) ([]models.Market, error) {
	const op = "Service.getMarketsWithSingleFlight"
	const key = "market:cache:all"

	result, err, _ := s.singleFlight.Do(key, func() (interface{}, error) {
		return s.loadAndWarmCache(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return result.([]models.Market), nil
}

func (s *Service) loadAndWarmCache(ctx context.Context) ([]models.Market, error) {
	const op = "Service.loadAndWarmCache"

	if markets, err := s.getMarketsFromCache(ctx); err == nil {
		return markets, nil
	}

	markets, err := s.getMarketsFromRepo(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	sort.Slice(markets, func(i, j int) bool {
		return markets[i].Name < markets[j].Name
	})

	if err = s.marketCacheRepository.SetAll(ctx, markets, s.cacheTTL); err != nil {
		zapLogger.Warn(ctx, "failed to update cache", zap.Error(err))
	}

	return markets, nil
}

func (s *Service) getMarketsFromRepo(ctx context.Context) ([]models.Market, error) {
	const op = "Service.getMarketsFromRepo"

	markets, err := s.marketRepository.ListAll(ctx)
	if err != nil {
		if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
			return nil, serviceErrors.ErrMarketsNotFound
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return markets, nil
}

func (s *Service) filterByRoles(markets []models.Market, userRoles []models.UserRole) []models.Market {
	isUser, isViewer := false, false

	for _, userRole := range userRoles {
		switch userRole {
		// Админ видит все рынки (включая disabled и deleted)
		case models.UserRoleAdmin:
			return markets
		case models.UserRoleUser:
			isUser = true
		case models.UserRoleViewer:
			isViewer = true
		default:
		}
	}

	out := make([]models.Market, 0, len(markets))
	for _, market := range markets {
		switch {
		// Viewer видит все неудаленные рынки (включая disabled)
		case isViewer && market.DeletedAt == nil:
			out = append(out, market)

		// User видит только enabled и неудаленные рынки
		case isUser && market.DeletedAt == nil && market.Enabled:
			out = append(out, market)
		}
	}

	return out
}
