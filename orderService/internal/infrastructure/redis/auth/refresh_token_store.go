package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const (
	refreshPrefix = "refresh"
	sessionPrefix = "auth_session"
)

// Атомарно проверяет старый refresh token, записывает новый с TTL и удаляет старый
var rotateScript = redisGo.NewScript(`
local oldRefreshKey = KEYS[1]
local newRefreshKey = KEYS[2]
local sessionKey = KEYS[3]
local ttlMs = ARGV[1]
local oldSessionID = ARGV[2]
local newSessionID = ARGV[3]

if redis.call("GET", sessionKey) ~= oldSessionID then
    return 0
end

if redis.call("EXISTS", oldRefreshKey) == 0 then
    return 0
end

redis.call("PSETEX", newRefreshKey, ttlMs, "1")
redis.call("PSETEX", sessionKey, ttlMs, newSessionID)
redis.call("DEL", oldRefreshKey)
return 1
`)

var replaceScript = redisGo.NewScript(`
local refreshKey = KEYS[1]
local sessionKey = KEYS[2]
local ttlMs = ARGV[1]
local sessionID = ARGV[2]

redis.call("PSETEX", refreshKey, ttlMs, "1")
redis.call("PSETEX", sessionKey, ttlMs, sessionID)
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

func (s *RefreshTokenStore) Replace(
	ctx context.Context,
	userID uuid.UUID,
	newJTI, newSessionID string,
) error {
	_, err := replaceScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{
			RefreshKey(userID, newJTI),
			SessionKey(userID),
		},
		s.ttl.Milliseconds(),
		newSessionID,
	).Result()
	if err != nil {
		return fmt.Errorf("replace refresh token: lua script error: %w", err)
	}

	return nil
}

func (s *RefreshTokenStore) Rotate(
	ctx context.Context,
	userID uuid.UUID,
	oldJTI, oldSessionID string,
	newJTI, newSessionID string,
) (bool, error) {
	result, err := rotateScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{
			RefreshKey(userID, oldJTI),
			RefreshKey(userID, newJTI),
			SessionKey(userID),
		},
		s.ttl.Milliseconds(),
		oldSessionID,
		newSessionID,
	).Int()
	if err != nil {
		return false, fmt.Errorf("rotate refresh token: lua script error: %w", err)
	}

	return result == 1, nil
}

func (s *RefreshTokenStore) IsSessionActive(
	ctx context.Context,
	userID uuid.UUID,
	sessionID string,
) (bool, error) {
	raw, err := s.store.Get(ctx, SessionKey(userID))
	if errors.Is(err, sharedErrors.ErrCacheNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get active session: %w", err)
	}

	return string(raw) == sessionID, nil
}

func RefreshKey(userID uuid.UUID, jti string) string {
	return fmt.Sprintf("%s:%s:%s", refreshPrefix, userID, jti)
}

func SessionKey(userID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", sessionPrefix, userID)
}
