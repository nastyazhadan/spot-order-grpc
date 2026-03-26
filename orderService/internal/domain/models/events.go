package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
)

type (
	OutboxEventStatus string
	InboxEventStatus  string
)

const (
	OutboxEventStatusPending    OutboxEventStatus = "pending"
	OutboxEventStatusProcessing OutboxEventStatus = "processing"
	OutboxEventStatusPublished  OutboxEventStatus = "published"
	OutboxEventStatusFailed     OutboxEventStatus = "failed"

	InboxEventStatusProcessing InboxEventStatus = "processing"
	InboxEventStatusProcessed  InboxEventStatus = "processed"
	InboxEventStatusFailed     InboxEventStatus = "failed"

	OrderCreatedEventType       = "order.created"
	OrderStatusUpdatedEventType = "order.status.updated"
)

// OrderCreatedEvent публикуется в Kafka через Transactional Outbox
type OrderCreatedEvent struct {
	EventID       uuid.UUID
	OrderID       uuid.UUID
	UserID        uuid.UUID
	MarketID      uuid.UUID
	Type          shared.OrderType
	Price         shared.Decimal
	Quantity      int64
	Status        shared.OrderStatus
	CorrelationID uuid.UUID
	CausationID   *uuid.UUID
	CreatedAt     time.Time
}

// OrderStatusUpdatedEvent публикуется в Kafka через Transactional Outbox
// после фактического committed-изменения статуса ордера.
type OrderStatusUpdatedEvent struct {
	EventID       uuid.UUID
	OrderID       uuid.UUID
	NewStatus     shared.OrderStatus
	Reason        string
	CorrelationID uuid.UUID
	CausationID   *uuid.UUID
	UpdatedAt     time.Time
}

// InboxEvent используется для дедупликации
type InboxEvent struct {
	ID            uuid.UUID        `db:"id"`
	EventID       uuid.UUID        `db:"event_id"`
	Topic         string           `db:"topic"`
	ConsumerGroup string           `db:"consumer_group"`
	Payload       []byte           `db:"payload"`
	Status        InboxEventStatus `db:"status"`
	ReceivedAt    time.Time        `db:"received_at"`
	ProcessedAt   *time.Time       `db:"processed_at"`
	FailedAt      *time.Time       `db:"failed_at"`
	ErrorMessage  *string          `db:"error_message"`
}

// OutboxEvent cоздается атомарно вместе с бизнес-сущностью в одной транзакции
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
