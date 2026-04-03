package jwt

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func ParseUserRolesClaims(rawRoles []string) ([]models.UserRole, error) {
	if len(rawRoles) == 0 {
		return nil, status.Error(codes.Unauthenticated, "user_roles not found in token")
	}

	out := make([]models.UserRole, 0, len(rawRoles))
	seen := make(map[models.UserRole]struct{}, len(rawRoles))

	for _, raw := range rawRoles {
		role, ok := models.ParseUserRole(raw)
		if !ok || role == models.UserRoleUnspecified {
			return nil, status.Error(codes.Unauthenticated, "invalid user_roles in token")
		}

		if _, exists := seen[role]; exists {
			continue
		}

		seen[role] = struct{}{}
		out = append(out, role)
	}

	if len(out) == 0 {
		return nil, status.Error(codes.Unauthenticated, "user_roles not found in token")
	}

	return out, nil
}

func UserRolesToClaims(roles []models.UserRole) ([]string, error) {
	if len(roles) == 0 {
		return nil, status.Error(codes.Internal, "failed to build token claims")
	}

	out := make([]string, 0, len(roles))
	seen := make(map[string]struct{}, len(roles))

	for _, role := range roles {
		if role == models.UserRoleUnspecified {
			continue
		}

		roleString := role.String()
		if _, exists := seen[roleString]; exists {
			continue
		}

		seen[roleString] = struct{}{}
		out = append(out, roleString)
	}

	if len(out) == 0 {
		return nil, status.Error(codes.Internal, "failed to build token claims")
	}

	return out, nil
}
