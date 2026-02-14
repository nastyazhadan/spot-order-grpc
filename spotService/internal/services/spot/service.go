package spot

import (
	"context"
	"fmt"
	"sort"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type Service struct {
	marketViewer MarketViewer
}

type MarketViewer interface {
	ListAll(ctx context.Context) ([]models.Market, error)
}

func NewService(marketProvider MarketViewer) *Service {
	return &Service{
		marketViewer: marketProvider,
	}
}

func (s *Service) ViewMarkets(ctx context.Context, userRoles []int32) ([]models.Market, error) {
	const op = "ViewMarkets"

	// TODO: добавить проверку ролей

	markets, err := s.marketViewer.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	out := make([]models.Market, 0, len(markets))
	for _, market := range markets {
		if !market.Enabled {
			continue
		}

		if market.DeletedAt != nil {
			continue
		}

		out = append(out, market)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out, nil
}
