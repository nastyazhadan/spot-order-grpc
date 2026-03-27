package market

import (
	"context"
	"errors"
	"fmt"
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

var syncMarketBlockStateScript = redisGo.NewScript(`
	local key = KEYS[1]
	local newTs = tonumber(ARGV[1])
	local newState = ARGV[2]

	local current = redis.call("GET", key)
	if current then
		local sep = string.find(current, ":")
		if sep then
			local oldTs = tonumber(string.sub(current, 1, sep - 1))
			if oldTs and oldTs > newTs then
				return 0
			end
		end
	end

	redis.call("SET", key, tostring(newTs) .. ":" .. newState)
	return 1
`)

type MarketBlockStore struct {
	store *cache.Store
}

func New(store *cache.Store) *MarketBlockStore {
	return &MarketBlockStore{
		store: store,
	}
}

func (s *MarketBlockStore) SyncState(
	ctx context.Context,
	marketID uuid.UUID,
	blocked bool,
	updatedAt time.Time,
) error {
	const op = "redis.MarketBlockStore.SyncState"

	state := unblockedState
	if blocked {
		state = blockedState
	}

	timestamp := updatedAt.UTC().UnixMicro()

	if _, err := syncMarketBlockStateScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{blockKey(marketID)},
		timestamp,
		state,
	).Result(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *MarketBlockStore) IsBlocked(ctx context.Context, marketID uuid.UUID) (bool, error) {
	const op = "redis.MarketBlockStore.IsBlocked"

	raw, err := s.store.Get(ctx, blockKey(marketID))
	if errors.Is(err, sharedErrors.ErrCacheNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	blocked, parseErr := parseBlockedState(raw)
	if parseErr != nil {
		return false, fmt.Errorf("%s: %w", op, parseErr)
	}

	return blocked, nil
}

func parseBlockedState(raw []byte) (bool, error) {
	value := string(raw)

	switch value {
	case blockedState:
		return true, nil
	case unblockedState:
		return false, nil
	}

	_, state, found := strings.Cut(value, ":")
	if !found {
		return false, fmt.Errorf("unexpected market block value %q", value)
	}

	switch state {
	case blockedState:
		return true, nil
	case unblockedState:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected market block state %q", state)
	}
}

func blockKey(marketID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", blockKeyPrefix, marketID.String())
}
