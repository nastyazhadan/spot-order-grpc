package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"
)

// для решения проблемы race condition между incr и count == 1
var rateLimitScript = redisGo.NewScript(`
	local count = redis.call('INCR', KEYS[1])
	if count == 1 then
		redis.call('EXPIRE', KEYS[1], ARGV[1])
	end
	return count
`)

type OrderRateLimiter struct {
	client *redisGo.Client
	limit  int64
	window time.Duration
	prefix string
}

func NewOrderRateLimiter(
	client *redisGo.Client,
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

	result, err := rateLimitScript.Run(ctx, r.client, []string{key}, int(r.window.Seconds())).Int64()
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	return result <= r.limit, nil
}
