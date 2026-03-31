package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const tokenPrefix = "refresh"

func Key(userID uuid.UUID, jti string) string {
	return fmt.Sprintf("%s:%s:%s", tokenPrefix, userID, jti)
}

func Save(
	ctx context.Context,
	store *cache.Store,
	ttl time.Duration,
	userID uuid.UUID,
	jti string,
) error {
	if err := store.SetWithTTL(ctx, Key(userID, jti), "refresh_token_v1", ttl); err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}

	return nil
}
