package models

import "time"

type Market struct {
	ID        string
	Enabled   bool
	DeletedAt *time.Time
}
