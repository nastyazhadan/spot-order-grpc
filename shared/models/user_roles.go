package models

type UserRole uint8

const (
	UserRoleUnspecified UserRole = iota
	UserRoleUser
	UserRoleAdmin
	UserRoleViewer
)
