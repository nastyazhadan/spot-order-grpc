package models

type UserRole uint8

const (
	RoleUnspecified UserRole = iota
	RoleUser
	RoleAdmin
	RoleViewer
)
