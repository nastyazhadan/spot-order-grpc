package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const sessionPrefix = "auth_session"

type Store struct {
	store *cache.Store
}

func New(store *cache.Store) *Store {
	return &Store{store: store}
}

func (s *Store) IsSessionActive(
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

func SessionKey(userID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", sessionPrefix, userID)
}
