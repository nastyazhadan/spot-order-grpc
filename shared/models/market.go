package models

import (
	"time"

	"github.com/google/uuid"
)

type Market struct {
	ID        uuid.UUID
	Name      string
	Enabled   bool
	DeletedAt *time.Time
}
