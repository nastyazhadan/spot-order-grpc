package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

type OrderRateLimiter struct {
	client cache.Client
	limit  int64
	window time.Duration
	prefix string
}

func NewOrderRateLimiter(
	client cache.Client,
	limit int64,
	window time.Duration,
	prefix string,
) *OrderRateLimiter {
	return &OrderRateLimiter{
		client: client,
		limit:  limit,
		window: window,
		prefix: prefix,
	}
}

func (r *OrderRateLimiter) Allow(ctx context.Context, userID uuid.UUID) (bool, error) {
	const op = "OrderRateLimiter.Allow"

	key := r.prefix + userID.String()

	count, err := r.client.Incr(ctx, key)
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	if count == 1 {
		if err := r.client.Expire(ctx, key, r.window); err != nil {
			return false, fmt.Errorf("%s: %w", op, err)
		}
	}

	return count <= r.limit, nil
}
