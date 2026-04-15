package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	redisGo "github.com/redis/go-redis/v9"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const keyPrefix = "idem:order:create"

const (
	statusProcessing = "processing"
	statusCompleted  = "completed"
)

type Entry struct {
	Status      string    `json:"status"`
	RequestHash string    `json:"request_hash"`
	OrderID     uuid.UUID `json:"order_id,omitempty"`
	OrderStatus string    `json:"order_status,omitempty"`
}

func (e Entry) IsCompleted() bool {
	return e.Status == statusCompleted
}

func (e Entry) IsProcessing() bool {
	return e.Status == statusProcessing
}

// - GET key → если есть, возвращает текущее значение (без изменений)
// - Если нет → SET key value PX ttlMs и возвращает ""
var acquireScript = redisGo.NewScript(`
    local key   = KEYS[1]
    local value = ARGV[1]
    local ttlMs = tonumber(ARGV[2])

    local existing = redis.call('GET', key)
    if existing then
        return existing
    end

    redis.call('SET', key, value, 'PX', ttlMs)
    return ""
`)

// Обновляет значение ключа, сохраняя оставшийся TTL.
// Возвращает 1 — успех, 0 — ключ исчез (TTL истёк)
var completeScript = redisGo.NewScript(`
    local key      = KEYS[1]
    local newValue = ARGV[1]

    local pttl = redis.call('PTTL', key)
    if pttl == -2 then
        return 0
    elseif pttl == -1 then
        redis.call('SET', key, newValue)
    else
        redis.call('SET', key, newValue, 'PX', pttl)
    end
    return 1
`)

type Store struct {
	store *cache.Store
	ttl   time.Duration
}

func New(store *cache.Store, ttl time.Duration) *Store {
	return &Store{store: store, ttl: ttl}
}

// Acquire атомарно захватывает ключ идемпотентности:
// - acquired=true  — ключ только что создан нами, можно создавать заказ
// - acquired=false — ключ уже существовал, entry содержит его состояние
func (s *Store) Acquire(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
) (Entry, bool, error) {
	const op = "IdempotencyStore.Acquire"

	key := buildKey(userID, requestHash)

	newEntry := Entry{Status: statusProcessing, RequestHash: requestHash}
	newValue, err := json.Marshal(newEntry)
	if err != nil {
		return Entry{}, false, fmt.Errorf("%s: marshal: %w", op, err)
	}

	raw, err := acquireScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{key},
		string(newValue),
		s.ttl.Milliseconds(),
	).Text()

	if err != nil && !errors.Is(err, redisGo.Nil) {
		return Entry{}, false, fmt.Errorf("%s: script: %w", op, err)
	}

	if raw == "" {
		return Entry{}, true, nil
	}

	var existing Entry
	if err = json.Unmarshal([]byte(raw), &existing); err != nil {
		_ = s.store.Delete(ctx, key)
		return Entry{}, false, fmt.Errorf(
			"%s: corrupted entry, cleared: %w", op, sharedErrors.ErrCacheNotFound,
		)
	}

	return existing, false, nil
}

// Complete помечает ключ как завершённый, сохраняя оставшийся TTL
func (s *Store) Complete(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
	orderID uuid.UUID,
	orderStatus string,
) error {
	const op = "IdempotencyStore.Complete"

	key := buildKey(userID, requestHash)
	entry := Entry{
		Status:      statusCompleted,
		RequestHash: requestHash,
		OrderID:     orderID,
		OrderStatus: orderStatus,
	}
	value, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("%s: marshal: %w", op, err)
	}

	result, err := completeScript.Run(
		ctx,
		s.store.ScriptRunner(),
		[]string{key},
		string(value),
	).Int64()
	if err != nil {
		return fmt.Errorf("%s: script: %w", op, err)
	}
	if result == 0 {
		if err = s.store.SetWithTTL(ctx, key, string(value), s.ttl); err != nil {
			return fmt.Errorf("%s: restore expired key: %w", op, err)
		}
	}

	return nil
}

// FailCleanup удаляет ключ, позволяя клиенту повторить запрос.
func (s *Store) FailCleanup(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
) error {
	const op = "IdempotencyStore.FailCleanup"
	if err := s.store.Delete(ctx, buildKey(userID, requestHash)); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func buildKey(userID uuid.UUID, requestHash string) string {
	return fmt.Sprintf("%s:%s:%s", keyPrefix, userID.String(), requestHash)
}
