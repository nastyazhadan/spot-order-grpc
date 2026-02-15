package spot

import (
	"context"
	"errors"
	"fmt"

	storageErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type Service struct {
	marketViewer MarketViewer
}

type MarketViewer interface {
	ListAll(ctx context.Context) ([]models.Market, error)
}

func NewService(marketViewer MarketViewer) *Service {
	return &Service{
		marketViewer: marketViewer,
	}
}

func (service *Service) ViewMarkets(ctx context.Context, userRoles []models.UserRole) ([]models.Market, error) {
	const op = "service.MarketViewer.ViewMarkets"

	markets, err := service.marketViewer.ListAll(ctx)
	if err != nil {
		if errors.Is(err, storageErrors.ErrMarketStoreIsEmpty) {
			return []models.Market{}, serviceErrors.ErrMarketsNotFound
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	isAdmin := false
	isUser := false
	isViewer := false

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
		// Админ видит все рынки (включая disabled и deleted)
		if isAdmin {
			out = append(out, market)
			continue
		}

		// Viewer видит все неудаленные рынки (включая disabled)
		if isViewer && market.DeletedAt == nil {
			out = append(out, market)
			continue
		}

		// User видит только enabled и неудаленные рынки
		if isUser && market.DeletedAt == nil && market.Enabled {
			out = append(out, market)
			continue
		}
	}
	return out, nil
}
