package spot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

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

	markets, err := s.marketCacheRepository.GetAll(ctx)
	if err != nil {
		if !errors.Is(err, repositoryErrors.ErrMarketCacheNotFound) {
			zapLogger.Error(ctx, "internal redis error", zap.Error(err))
		}

		markets, err = s.marketRepository.ListAll(ctx)
		if err != nil {
			if errors.Is(err, repositoryErrors.ErrMarketStoreIsEmpty) {
				return nil, serviceErrors.ErrMarketsNotFound
			}

			return nil, fmt.Errorf("%s: %w", op, err)
		}

		_ = s.marketCacheRepository.SetAll(ctx, markets, s.cacheTTL)
	}

	return s.filterByRoles(markets, userRoles), nil
}

func (s *Service) filterByRoles(markets []models.Market, userRoles []models.UserRole) []models.Market {
	isAdmin, isUser, isViewer := false, false, false

	for _, userRole := range userRoles {
		switch userRole {
		case models.UserRoleAdmin:
			isAdmin = true
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
		// Админ видит все рынки (включая disabled и deleted)
		case isAdmin:
			out = append(out, market)

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
