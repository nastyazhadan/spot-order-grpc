package requestctx

import (
	"context"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

const userRolesKey contextKey = "user_roles"

func ContextWithUserRoles(ctx context.Context, roles []models.UserRole) (context.Context, bool) {
	if ctx == nil {
		return nil, false
	}

	copied := append([]models.UserRole(nil), roles...)
	return context.WithValue(ctx, userRolesKey, copied), true
}

func UserRolesFromContext(ctx context.Context) ([]models.UserRole, bool) {
	if ctx == nil {
		return nil, false
	}

	roles, ok := ctx.Value(userRolesKey).([]models.UserRole)
	if !ok || len(roles) == 0 {
		return nil, false
	}

	return append([]models.UserRole(nil), roles...), true
}
