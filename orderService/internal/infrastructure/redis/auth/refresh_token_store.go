package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"

	auth "github.com/nastyazhadan/spot-order-grpc/shared/auth/session"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const (
	refreshPrefix        = "refresh"
	currentRefreshPrefix = "auth_refresh"
)

// Атомарно валидирует активную refresh/session-связку,
// ротирует refresh token, обновляет session ID и указатель на текущий refresh key
var rotateScript = redisGo.NewScript(`
	local oldRefreshKey = KEYS[1]
	local newRefreshKey = KEYS[2]
	local sessionKey = KEYS[3]
	local currentRefreshPointerKey = KEYS[4]
	local ttlMs = ARGV[1]
	local oldSessionID = ARGV[2]
	local newSessionID = ARGV[3]

	if redis.call("GET", sessionKey) ~= oldSessionID then
    	return 0
	end

	if redis.call("EXISTS", oldRefreshKey) == 0 then
    	return 0
	end

	local trackedRefreshKey = redis.call("GET", currentRefreshPointerKey)
	if trackedRefreshKey and trackedRefreshKey ~= "" and trackedRefreshKey ~= oldRefreshKey then
		return 0
	end

	redis.call("PSETEX", newRefreshKey, ttlMs, "1")
	redis.call("PSETEX", sessionKey, ttlMs, newSessionID)
	redis.call("PSETEX", currentRefreshPointerKey, ttlMs, newRefreshKey)
	redis.call("DEL", oldRefreshKey)
	return 1
`)

var replaceScript = redisGo.NewScript(`
	local newRefreshKey = KEYS[1]
	local sessionKey = KEYS[2]
	local currentRefreshKey = KEYS[3]
	local ttlMs = ARGV[1]
	local sessionID = ARGV[2]

	local oldRefreshKey = redis.call("GET", currentRefreshKey)
	if oldRefreshKey and oldRefreshKey ~= "" then
		redis.call("DEL", oldRefreshKey)
	end

	redis.call("PSETEX", newRefreshKey, ttlMs, "1")
	redis.call("PSETEX", sessionKey, ttlMs, sessionID)
	redis.call("PSETEX", currentRefreshKey, ttlMs, newRefreshKey)

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
			refreshKey(userID, newJTI),
			auth.SessionKey(userID),
			currentRefreshKey(userID),
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
			refreshKey(userID, oldJTI),
			refreshKey(userID, newJTI),
			auth.SessionKey(userID),
			currentRefreshKey(userID),
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

func refreshKey(userID uuid.UUID, jti string) string {
	return fmt.Sprintf("%s:%s:%s", refreshPrefix, userID, jti)
}

func currentRefreshKey(userID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", currentRefreshPrefix, userID)
}
