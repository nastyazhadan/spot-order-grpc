package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/nastyazhadan/spot-order-grpc/orderService/errors/storage"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"

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

func (s *OrderStore) SaveOrder(ctx context.Context, order models.Order) error {
	const op = "storage.OrderStore.SaveOrder"

	select {
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", op, ctx.Err())
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, found := s.order[order.ID]; found {
		return fmt.Errorf("%s: %w", op, storage.ErrOrderAlreadyExists)
	}

	s.order[order.ID] = order
	return nil
}

func (s *OrderStore) GetOrder(ctx context.Context, id uuid.UUID) (models.Order, error) {
	const op = "storage.OrderStore.GetOrder"

	select {
	case <-ctx.Done():
		return models.Order{}, fmt.Errorf("%s: %w", op, ctx.Err())
	default:
	}

	s.mu.RLock()
	result, found := s.order[id]
	s.mu.RUnlock()

	if !found {
		return models.Order{}, fmt.Errorf("%s: %w", op, storage.ErrOrderNotFound)
	}

	return result, nil
}
