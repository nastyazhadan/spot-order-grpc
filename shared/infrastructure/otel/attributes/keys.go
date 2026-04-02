package attributes

const (
	OrderID              = "order.id"
	OrderType            = "order.type"
	OrderStatus          = "order.status"
	OrdersCancelledCount = "orders.cancelled_count"

	UserID      = "user.id"
	UserRoleKey = "user.role_key"
	MarketID    = "market.id"

	OperationName = "operation.name"

	CacheTTL                = "cache.ttl_seconds"
	CacheCorrupted          = "cache.corrupted"
	CacheCorruptedReason    = "cache.corrupted_reason"
	CacheInvalidationFailed = "cache.invalidation_failed"

	KafkaTopic               = "kafka.topic"
	KafkaPartition           = "kafka.partition"
	KafkaOffset              = "kafka.offset"
	ConsumerGroup            = "kafka.consumer_group"
	MessagingSystem          = "messaging.system"
	MessagingDestination     = "messaging.destination"
	MessagingDestinationKind = "messaging.destination_kind"

	EventID            = "event.id"
	EventType          = "event.type"
	RetryCount         = "retry.count"
	BatchSize          = "batch.size"
	BatchLimit         = "batch.limit"
	AggregateID        = "aggregate.id"
	OutboxID           = "outbox.id"
	StuckEvent         = "stuck.event"
	ReleasedEventCount = "released.count"

	DBSystem = "db.system"

	MarketEnabled         = "market.enabled"
	MarketDeleted         = "market.deleted"
	MarketBlocked         = "market.blocked"
	MarketBlockSyncFailed = "market.block_sync_failed"
	MarketBlockSyncReason = "market.block_sync_reason"
	MarketsCount          = "markets.count"
)
