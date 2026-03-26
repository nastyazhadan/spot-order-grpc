package models

import (
	"time"

	"github.com/google/uuid"
)

type OutboxEventStatus string

const (
	OutboxEventStatusPending    OutboxEventStatus = "pending"
	OutboxEventStatusProcessing OutboxEventStatus = "processing"
	OutboxEventStatusPublished  OutboxEventStatus = "published"
	OutboxEventStatusFailed     OutboxEventStatus = "failed"
)

type OutboxEvent struct {
	ID          uuid.UUID         `db:"id"`
	EventID     uuid.UUID         `db:"event_id"`
	EventType   string            `db:"event_type"`
	AggregateID uuid.UUID         `db:"aggregate_id"`
	Payload     []byte            `db:"payload"`
	Status      OutboxEventStatus `db:"status"`
	RetryCount  int               `db:"retry_count"`
	AvailableAt time.Time         `db:"available_at"`
	CreatedAt   time.Time         `db:"created_at"`
	PublishedAt *time.Time        `db:"published_at"`
	FailedAt    *time.Time        `db:"failed_at"`
	LockedAt    *time.Time        `db:"locked_at"`
	LastError   *string           `db:"last_error"`
}
