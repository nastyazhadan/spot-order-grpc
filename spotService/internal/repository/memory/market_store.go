package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

type MarketStore struct {
	markets map[string]models.Market
	mu      sync.RWMutex
}

func NewMarketStore() *MarketStore {
	marketStore := &MarketStore{
		markets: make(map[string]models.Market),
	}
	marketStore.fill()
	return marketStore
}

func (marketStore *MarketStore) fill() {
	now := time.Now()

	marketStore.markets["BTC"] = models.Market{
		ID:        uuid.New(),
		Name:      "BTC-USDT",
		Enabled:   true,
		DeletedAt: nil,
	}

	marketStore.markets["ETH"] = models.Market{
		ID:        uuid.New(),
		Name:      "ETH-USDT",
		Enabled:   false,
		DeletedAt: nil,
	}

	marketStore.markets["DOGE"] = models.Market{
		ID:        uuid.New(),
		Name:      "DOGE-USDT",
		Enabled:   true,
		DeletedAt: &now,
	}

	marketStore.markets["SOL"] = models.Market{
		ID:        uuid.New(),
		Name:      "SOL-USDT",
		Enabled:   true,
		DeletedAt: nil,
	}

	marketStore.markets["ADA"] = models.Market{
		ID:        uuid.New(),
		Name:      "ADA-USDT",
		Enabled:   false,
		DeletedAt: nil,
	}
}

func (marketStore *MarketStore) ListAll(ctx context.Context) ([]models.Market, error) {
	const op = "repository.MarketStore.ListAll"

	marketStore.mu.RLock()
	defer marketStore.mu.RUnlock()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", op, ctx.Err())
	default:
	}

	marketsCount := len(marketStore.markets)
	if marketsCount == 0 {
		return []models.Market{}, fmt.Errorf("%s: %w", op, repository.ErrMarketStoreIsEmpty)
	}

	out := make([]models.Market, 0, marketsCount)

	for _, market := range marketStore.markets {
		out = append(out, market)
	}

	return out, nil
}
