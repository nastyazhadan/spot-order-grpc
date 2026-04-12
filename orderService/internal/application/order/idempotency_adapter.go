package order

import (
	"context"

	"github.com/google/uuid"

	idempotencyStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/idempotency"
	orderService "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
)

type redisIdempotencyAdapter struct {
	store *idempotencyStore.Store
}

func (a *redisIdempotencyAdapter) Acquire(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
) (orderService.IdempotencyResult, bool, error) {
	entry, acquired, err := a.store.Acquire(ctx, userID, requestHash)
	if err != nil {
		return orderService.IdempotencyResult{}, false, err
	}
	return orderService.IdempotencyResult{
		IsCompleted:  entry.IsCompleted(),
		IsProcessing: entry.IsProcessing(),
		OrderID:      entry.OrderID,
		OrderStatus:  entry.OrderStatus,
	}, acquired, nil
}

func (a *redisIdempotencyAdapter) Complete(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
	orderID uuid.UUID,
	orderStatus string,
) error {
	return a.store.Complete(ctx, userID, requestHash, orderID, orderStatus)
}

func (a *redisIdempotencyAdapter) FailCleanup(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
) error {
	return a.store.FailCleanup(ctx, userID, requestHash)
}
