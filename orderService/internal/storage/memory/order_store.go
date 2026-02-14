package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/shared/errors/storage"

	"github.com/google/uuid"
)

type OrderStore struct {
	order map[uuid.UUID]models.Order
	mu    sync.RWMutex
}

func NewOrderStore() *OrderStore {
	return &OrderStore{
		order: make(map[uuid.UUID]models.Order, 1024),
	}
}

func (store *OrderStore) SaveOrder(ctx context.Context, order models.Order) error {
	const op = "storage.OrderStore.SaveOrder"

	select {
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", op, ctx.Err())
	default:
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if _, found := store.order[order.ID]; found {
		return fmt.Errorf("%s: %w", op, storage.ErrOrderAlreadyExists)
	}

	store.order[order.ID] = order
	return nil
}

func (store *OrderStore) GetOrder(ctx context.Context, id uuid.UUID) (models.Order, error) {
	const op = "storage.OrderStore.GetOrder"

	select {
	case <-ctx.Done():
		return models.Order{}, fmt.Errorf("%s: %w", op, ctx.Err())
	default:
	}

	store.mu.RLock()
	result, found := store.order[id]
	store.mu.RUnlock()

	if !found {
		return models.Order{}, fmt.Errorf("%s: %w", op, storage.ErrOrderNotFound)
	}

	return result, nil
}
