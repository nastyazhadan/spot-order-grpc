package memory

import (
	"context"
	"fmt"
	"spotOrder/internal/domain/models"
	"sync"
	"time"
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

func (ms *MarketStore) fill() {
	now := time.Now()

	ms.markets["BTC-USDT"] = models.Market{
		ID:        "BTC-USDT",
		Enabled:   true,
		DeletedAt: nil,
	}
	ms.markets["ETH-USDT"] = models.Market{
		ID:        "ETH-USDT",
		Enabled:   false,
		DeletedAt: nil,
	}
	ms.markets["DOGE-USDT"] = models.Market{
		ID:        "DOGE-USDT",
		Enabled:   true,
		DeletedAt: &now,
	}
}

func (ms *MarketStore) ListAll(ctx context.Context) ([]models.Market, error) {
	const op = "storage.MarketStore.ListAll"

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", op, ctx.Err())
	default:
	}

	if len(ms.markets) == 0 {
		return nil, nil
	}

	out := make([]models.Market, 0, len(ms.markets))

	for _, market := range ms.markets {
		out = append(out, market)
	}

	return out, nil
}
