package market

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const blockKeyPrefix = "market:block"

type MarketBlockStore struct {
	store *cache.Store
}

func New(store *cache.Store) *MarketBlockStore {
	return &MarketBlockStore{
		store: store,
	}
}

func (s *MarketBlockStore) Block(ctx context.Context, marketID uuid.UUID) error {
	const op = "redis.market.MarketBlockStore.Block"

	if err := s.store.Set(ctx, blockKey(marketID), "1"); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *MarketBlockStore) Unblock(ctx context.Context, marketID uuid.UUID) error {
	const op = "redis.market.MarketBlockStore.Unblock"

	if err := s.store.Delete(ctx, blockKey(marketID)); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *MarketBlockStore) IsBlocked(ctx context.Context, marketID uuid.UUID) (bool, error) {
	const op = "redis.market.MarketBlockStore.IsBlocked"

	blocked, err := s.store.Exists(ctx, blockKey(marketID))
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	return blocked, nil
}

func blockKey(marketID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", blockKeyPrefix, marketID.String())
}
