package models

import "strings"

type UserRole uint16

const (
	UserRoleUnspecified UserRole = iota
	UserRoleUser
	UserRoleAdmin
	UserRoleViewer
)

func (r UserRole) String() string {
	switch r {
	case UserRoleUser:
		return "ROLE_USER"
	case UserRoleAdmin:
		return "ROLE_ADMIN"
	case UserRoleViewer:
		return "ROLE_VIEWER"
	default:
		return "ROLE_UNSPECIFIED"
	}
}

func ParseUserRole(value string) (UserRole, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ROLE_USER":
		return UserRoleUser, true
	case "ROLE_ADMIN":
		return UserRoleAdmin, true
	case "ROLE_VIEWER":
		return UserRoleViewer, true
	default:
		return UserRoleUnspecified, false
	}
}
