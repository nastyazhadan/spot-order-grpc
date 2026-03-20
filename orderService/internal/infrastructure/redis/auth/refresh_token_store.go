package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"
)

const refreshTokenPrefix = "refresh"

// Атомарно проверяет наличие ключа и удаляет его
var revokeIfExistsScript = redisGo.NewScript(`
local key = KEYS[1]
if redis.call("EXISTS", key) == 1 then
    redis.call("DEL", key)
    return 1
end
return 0
`)

type RefreshTokenStore struct {
	store *cache.Store
	ttl   time.Duration
}

func NewRefreshTokenStore(store *cache.Store, ttl time.Duration) *RefreshTokenStore {
	return &RefreshTokenStore{
		store: store,
		ttl:   ttl,
	}
}

func (s *RefreshTokenStore) Save(ctx context.Context, userID uuid.UUID, jti string) error {
	return s.store.SetWithTTL(ctx, s.key(userID, jti), "1", s.ttl)
}

func (s *RefreshTokenStore) RevokeIfExists(ctx context.Context, userID uuid.UUID, jti string) (bool, error) {
	result, err := revokeIfExistsScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{s.key(userID, jti)},
	).Int()
	if err != nil {
		return false, fmt.Errorf("revoke if exists: lua script error: %w", err)
	}

	return result == 1, nil
}

func (s *RefreshTokenStore) key(userID uuid.UUID, jti string) string {
	return fmt.Sprintf("%s:%s:%s", refreshTokenPrefix, userID, jti)
}
