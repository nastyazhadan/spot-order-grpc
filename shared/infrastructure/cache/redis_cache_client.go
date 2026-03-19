package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	redis *redis.Client
}

func New(redisClient *redis.Client) *Store {
	return &Store{
		redis: redisClient,
	}
}

func (s *Store) ScriptRunner() redis.Scripter {
	return s.redis
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	return nil
}

func (s *Store) Set(ctx context.Context, key string, value interface{}) error {
	if err := s.redis.Set(ctx, key, value, 0).Err(); err != nil {
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}

	return nil
}

func (s *Store) SetWithTTL(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if err := s.redis.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set key %s with ttl: %w", key, err)
	}

	return nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	result, err := s.redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	return result, nil
}

func (s *Store) GetHashAll(ctx context.Context, key string) (map[string]string, bool, error) {
	result, err := s.hGetAll(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if len(result) == 0 {
		return nil, false, nil
	}

	return result, true, nil
}

func (s *Store) SetHash(ctx context.Context, key string, values map[string]any, ttl time.Duration) error {
	if err := s.hSet(ctx, key, values); err != nil {
		return err
	}

	if ttl > 0 {
		if err := s.expire(ctx, key, ttl); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	return nil
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	result, err := s.redis.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check if key %s exists: %w", key, err)
	}

	return result > 0, nil
}

func (s *Store) expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := s.redis.Expire(ctx, key, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set expiration for key %s: %w", key, err)
	}

	return nil
}

func (s *Store) hSet(ctx context.Context, key string, values any) error {
	if err := s.redis.HSet(ctx, key, values).Err(); err != nil {
		return fmt.Errorf("failed to hash set key %s: %w", key, err)
	}

	return nil
}

func (s *Store) hGetAll(ctx context.Context, key string) (map[string]string, error) {
	result, err := s.redis.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	return result, nil
}
