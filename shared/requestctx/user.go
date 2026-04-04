package requestctx

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const userIDKey contextKey = "user_id"

func ContextWithUserID(ctx context.Context, userID uuid.UUID) (context.Context, bool) {
	if ctx == nil {
		return nil, false
	}

	return context.WithValue(ctx, userIDKey, userID), true
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if ctx == nil {
		return uuid.Nil, false
	}

	userID, ok := ctx.Value(userIDKey).(uuid.UUID)
	return userID, ok
}
