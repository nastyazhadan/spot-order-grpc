package models

type UserRole uint16

const (
	UserRoleUnspecified UserRole = iota
	UserRoleUser
	UserRoleAdmin
	UserRoleViewer
)
