package models

type UserRole uint8

const (
	Unspecified UserRole = iota
	User
	Admin
	Viewer
)
