package jwt

import (
	authErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"
)

func ParseUserRolesClaims(rawRoles []string) ([]models.UserRole, error) {
	if len(rawRoles) == 0 {
		return nil, authErrors.ErrMissingUserRoles
	}

	out := make([]models.UserRole, 0, len(rawRoles))
	seen := make(map[models.UserRole]struct{}, len(rawRoles))

	for _, raw := range rawRoles {
		role, ok := models.ParseUserRole(raw)
		if !ok || role == models.UserRoleUnspecified {
			return nil, authErrors.ErrInvalidUserRoles
		}

		if _, exists := seen[role]; exists {
			continue
		}

		seen[role] = struct{}{}
		out = append(out, role)
	}

	if len(out) == 0 {
		return nil, authErrors.ErrMissingUserRoles
	}

	return out, nil
}

func UserRolesToClaims(roles []models.UserRole) ([]string, error) {
	if len(roles) == 0 {
		return nil, authErrors.ErrBuildTokenClaimsFailed
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
		return nil, authErrors.ErrBuildTokenClaimsFailed
	}

	return out, nil
}
