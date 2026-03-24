package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const refreshTokenPrefix = "refresh"

// Атомарно проверяет старый refresh token, записывает новый с TTL и удаляет старый
var rotateScript = redisGo.NewScript(`
local oldKey = KEYS[1]
local newKey = KEYS[2]
local ttlMs = ARGV[1]

if redis.call("EXISTS", oldKey) == 0 then
    return 0
end

redis.call("PSETEX", newKey, ttlMs, "1")
redis.call("DEL", oldKey)
return 1
`)

type RefreshTokenStore struct {
	store *cache.Store
	ttl   time.Duration
}

func New(store *cache.Store, ttl time.Duration) *RefreshTokenStore {
	return &RefreshTokenStore{
		store: store,
		ttl:   ttl,
	}
}

func (s *RefreshTokenStore) Rotate(ctx context.Context, userID uuid.UUID, oldJTI, newJTI string) (bool, error) {
	result, err := rotateScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{
			s.key(userID, oldJTI),
			s.key(userID, newJTI),
		},
		s.ttl.Milliseconds(),
	).Int()
	if err != nil {
		return false, fmt.Errorf("rotate refresh token: lua script error: %w", err)
	}

	return result == 1, nil
}

func (s *RefreshTokenStore) key(userID uuid.UUID, jti string) string {
	return fmt.Sprintf("%s:%s:%s", refreshTokenPrefix, userID, jti)
}
