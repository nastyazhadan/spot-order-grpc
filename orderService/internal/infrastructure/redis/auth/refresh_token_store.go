package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const refreshTokenPrefix = "refresh"

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

func (s *RefreshTokenStore) Exists(ctx context.Context, userID uuid.UUID, jti string) (bool, error) {
	return s.store.Exists(ctx, s.key(userID, jti))
}

func (s *RefreshTokenStore) Revoke(ctx context.Context, userID uuid.UUID, jti string) error {
	return s.store.Delete(ctx, s.key(userID, jti))
}

func (s *RefreshTokenStore) key(userID uuid.UUID, jti string) string {
	return fmt.Sprintf("%s:%s:%s", refreshTokenPrefix, userID, jti)
}
