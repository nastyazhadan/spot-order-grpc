package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/shared/infra/redis"
)

const orderRateLimitPrefix = "rate:order:create:"

type OrderRateLimiter struct {
	client redis.RedisClient
	limit  int64
	window time.Duration
}

func NewOrderRateLimiter(client redis.RedisClient, limit int64, window time.Duration) *OrderRateLimiter {
	return &OrderRateLimiter{
		client: client,
		limit:  limit,
		window: window,
	}
}

func (r *OrderRateLimiter) Allow(ctx context.Context, userID uuid.UUID) (bool, error) {
	key := orderRateLimitPrefix + userID.String()

	// INCR атомарно увеличивает счётчик и возвращает новое значение.
	// Если ключа не было — создаёт его со значением 1.
	count, err := r.client.Incr(ctx, key)
	if err != nil {
		return false, fmt.Errorf("rate limiter incr: %w", err)
	}

	// Устанавливаем TTL только при первом ордере (count == 1),
	// чтобы не сдвигать окно при каждом запросе.
	if count == 1 {
		if err := r.client.Expire(ctx, key, r.window); err != nil {
			return false, fmt.Errorf("rate limiter expire: %w", err)
		}
	}

	return count <= r.limit, nil
}
