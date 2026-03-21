package order

import (
	"context"
	"fmt"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"

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
	store  *cache.Store
	limit  int64
	window time.Duration
	prefix string
}

func NewOrderRateLimiter(
	store *cache.Store,
	limit int64,
	window time.Duration,
	prefix string,
) *OrderRateLimiter {
	return &OrderRateLimiter{
		store:  store,
		limit:  limit,
		window: window,
		prefix: prefix,
	}
}

func (r *OrderRateLimiter) Allow(ctx context.Context, userID uuid.UUID) (bool, error) {
	const op = "OrderRateLimiter.Allow"

	key := r.prefix + userID.String()

	raw, err := rateLimitScript.Run(
		ctx,
		r.store.ScriptRunner(),
		[]string{key},
		int64(r.window/time.Second),
	).Result()
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	count, ok := raw.(int64)
	if !ok {
		return false, fmt.Errorf("%s: unexpected script result type %T", op, raw)
	}

	return count <= r.limit, nil
}

func (r *OrderRateLimiter) Limit() int64 {
	return r.limit
}

func (r *OrderRateLimiter) Window() time.Duration {
	return r.window
}
