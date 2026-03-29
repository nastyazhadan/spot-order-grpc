package market

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const (
	blockKeyPrefix = "market:block"
	blockedState   = "1"
	unblockedState = "0"
)

var syncStateScript = redisGo.NewScript(`
	local key = KEYS[1]
	local newTs = tonumber(ARGV[1])
	local newState = ARGV[2]
	local ttlMs = tonumber(ARGV[3])

	local current = redis.call("GET", key)
	if not current then
		redis.call("SET", key, tostring(newTs) .. ":" .. newState, "PX", ttlMs)
		return 1
	end

	local sep = string.find(current, ":")
	if not sep then
		return redis.error_reply("invalid market block state")
	end

	local currentTs = tonumber(string.sub(current, 1, sep - 1))
	if not currentTs then
		return redis.error_reply("invalid market block timestamp")
	end

	if newTs < currentTs then
		return 0
	end

	redis.call("SET", key, tostring(newTs) .. ":" .. newState, "PX", ttlMs)
	return 1
`)

type MarketBlockStore struct {
	store *cache.Store
	ttl   time.Duration
}

func New(store *cache.Store, ttl time.Duration) *MarketBlockStore {
	return &MarketBlockStore{
		store: store,
		ttl:   ttl,
	}
}

func (s *MarketBlockStore) SyncState(
	ctx context.Context,
	marketID uuid.UUID,
	blocked bool,
	updatedAt time.Time,
) (bool, error) {
	const op = "redis.MarketBlockStore.SyncState"

	state := unblockedState
	if blocked {
		state = blockedState
	}
	ttlMs := s.ttl.Milliseconds()

	result, err := syncStateScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{blockKey(marketID)},
		updatedAt.UTC().UnixMilli(),
		state,
		ttlMs,
	).Result()
	if err != nil {
		return false, fmt.Errorf("%s: run sync state script: %w", op, err)
	}

	switch value := result.(type) {
	case int64:
		return value == 1, nil
	case string:
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			return false, fmt.Errorf("%s: unexpected script result: %q", op, value)
		}
		return parsed == 1, nil
	default:
		return false, fmt.Errorf("%s: unexpected script result type %T", op, result)
	}
}

func (s *MarketBlockStore) IsBlocked(ctx context.Context, marketID uuid.UUID) (bool, error) {
	const op = "redis.MarketBlockStore.IsBlocked"

	raw, err := s.store.Get(ctx, blockKey(marketID))
	if err != nil {
		if errors.Is(err, sharedErrors.ErrCacheNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("%s: get blocked state: %w", op, err)
	}

	blocked, _, parseErr := parseBlockedState(string(raw))
	if parseErr != nil {
		return false, fmt.Errorf("%s: parse blocked state: %w", op, parseErr)
	}

	return blocked, nil
}

func parseBlockedState(raw string) (bool, time.Time, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return false, time.Time{}, fmt.Errorf("invalid blocked state format: %q", raw)
	}

	tsMs, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("parse blocked state timestamp: %w", err)
	}

	var blocked bool
	switch parts[1] {
	case blockedState:
		blocked = true
	case unblockedState:
		blocked = false
	default:
		return false, time.Time{}, fmt.Errorf("invalid blocked state flag: %q", parts[1])
	}

	return blocked, time.UnixMilli(tsMs).UTC(), nil
}

func blockKey(marketID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", blockKeyPrefix, marketID.String())
}
