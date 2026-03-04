package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type client struct {
	redis *redis.Client
}

type Client interface {
	Set(ctx context.Context, key string, value interface{}) error
	SetWithTTL(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	HashSet(ctx context.Context, key string, value interface{}) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	Del(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Expire(ctx context.Context, key string, expirationTime time.Duration) error
	Ping(ctx context.Context) error
	RunScript(ctx context.Context, script *redis.Script, keys []string, args ...interface{}) (interface{}, error)
}

func NewClient(redisClient *redis.Client) *client {
	return &client{
		redis: redisClient,
	}
}

func (c *client) Set(ctx context.Context, key string, value interface{}) error {
	if err := c.redis.Set(ctx, key, value, 0).Err(); err != nil {
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}

	return nil
}

func (c *client) SetWithTTL(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if err := c.redis.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set key %s with ttl: %w", key, err)
	}

	return nil
}

func (c *client) Get(ctx context.Context, key string) ([]byte, error) {
	result, err := c.redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	return result, nil
}

func (c *client) HashSet(ctx context.Context, key string, values interface{}) error {
	if err := c.redis.HSet(ctx, key, values).Err(); err != nil {
		return fmt.Errorf("failed to hash set key %s: %w", key, err)
	}

	return nil
}

func (c *client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	result, err := c.redis.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	return result, nil
}

func (c *client) Del(ctx context.Context, key string) error {
	if err := c.redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	return nil
}

func (c *client) Exists(ctx context.Context, key string) (bool, error) {
	result, err := c.redis.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check if key %s exists: %w", key, err)
	}

	return result > 0, nil
}

func (c *client) Expire(ctx context.Context, key string, expiration time.Duration) error {
	if err := c.redis.Expire(ctx, key, expiration).Err(); err != nil {
		return fmt.Errorf("failed to set expiration for key %s: %w", key, err)
	}

	return nil
}

func (c *client) Ping(ctx context.Context) error {
	if err := c.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	return nil
}

func (c *client) RunScript(ctx context.Context, script *redis.Script, keys []string, args ...interface{}) (interface{}, error) {
	return script.Run(ctx, c.redis, keys, args...).Result()
}
