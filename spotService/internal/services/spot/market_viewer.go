package spot

import (
	"context"
	"errors"
	"fmt"
	"slices"

	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	storageErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/storage"
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

func (service *Service) ViewMarkets(ctx context.Context, userRoles []uint8) ([]models.Market, error) {
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
		switch models.UserRole(userRole) {
		case models.RoleAdmin:
			isAdmin = true
		case models.RoleUser:
			isUser = true
		case models.RoleViewer:
			isViewer = true
		default:
		}
	}

	out := make([]models.Market, 0, len(markets))
	for _, market := range markets {
		if isAdmin {
			out = append(out, market)
			continue
		}

		if isViewer && market.DeletedAt == nil {
			out = append(out, market)
			continue
		}

		if isUser && market.DeletedAt == nil && market.Enabled {
			out = append(out, market)
			continue
		}
	}
	return out, nil
}
