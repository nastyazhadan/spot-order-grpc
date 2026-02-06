package db

import (
	"context"
	"fmt"
	"spotOrder/internal/domain/models"
	"spotOrder/internal/storage"
)

type OrderStore struct {
	order map[string]models.Order
}

func NewOrderStore() *OrderStore {
	return &OrderStore{
		order: make(map[string]models.Order),
	}
}

func (s *OrderStore) SaveOrder(ctx context.Context, order models.Order) error {
	_ = ctx
	const op = "storage.OrderStore.SaveOrder"

	if _, found := s.order[order.ID]; found {
		return fmt.Errorf("%s: %w", op, storage.ErrOrderAlreadyExists)
	}

	s.order[order.ID] = order
	return nil
}

func (s *OrderStore) GetOrder(ctx context.Context, id string) (models.Order, error) {
	_ = ctx
	const op = "storage.OrderStore.GetOrder"

	order, found := s.order[id]
	if !found {
		return models.Order{}, fmt.Errorf("%s: %w", op, storage.ErrOrderNotFound)
	}

	return order, nil
}
