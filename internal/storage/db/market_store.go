package db

import (
	"context"
	"sort"
	"time"

	"spotOrder/internal/domain/models"
)

type MarketStore struct {
	markets map[string]models.Market
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
	_ = ctx

	if len(ms.markets) == 0 {
		return []models.Market{}, nil
	}

	out := make([]models.Market, 0, len(ms.markets))

	for _, m := range ms.markets {
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}
