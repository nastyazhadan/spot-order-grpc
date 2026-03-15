package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
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

	key := r.prefix + ":" + userID.String()

	raw, err := r.client.RunScript(ctx, rateLimitScript, []string{key}, int(r.window.Seconds()))
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	result, ok := raw.(int64)
	if !ok {
		return false, fmt.Errorf("%s: unexpected script result type", op)
	}

	return result <= r.limit, nil
}

func (r *OrderRateLimiter) Limit() int64 {
	return r.limit
}

func (r *OrderRateLimiter) Window() time.Duration {
	return r.window
}
