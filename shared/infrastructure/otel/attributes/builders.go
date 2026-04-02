package attributes

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
)

func OrderIDValue(v string) attribute.KeyValue     { return attribute.String(OrderID, v) }
func OrderTypeValue(v string) attribute.KeyValue   { return attribute.String(OrderType, v) }
func OrderStatusValue(v string) attribute.KeyValue { return attribute.String(OrderStatus, v) }
func OrdersCancelledCountValue(v int) attribute.KeyValue {
	return attribute.Int(OrdersCancelledCount, v)
}

func UserIDValue(v string) attribute.KeyValue      { return attribute.String(UserID, v) }
func MarketIDValue(v string) attribute.KeyValue    { return attribute.String(MarketID, v) }
func UserRoleKeyValue(v string) attribute.KeyValue { return attribute.String(UserRoleKey, v) }

func OperationNameValue(v string) attribute.KeyValue { return attribute.String(OperationName, v) }

func CacheTTLValue(v time.Duration) attribute.KeyValue {
	return attribute.Int64(CacheTTL, int64(v.Seconds()))
}
func CacheCorruptedValue(v bool) attribute.KeyValue { return attribute.Bool(CacheCorrupted, v) }
func CacheCorruptedReasonValue(v string) attribute.KeyValue {
	return attribute.String(CacheCorruptedReason, v)
}

func CacheInvalidationFailedValue(v bool) attribute.KeyValue {
	return attribute.Bool(CacheInvalidationFailed, v)
}

func KafkaTopicValue(v string) attribute.KeyValue { return attribute.String(KafkaTopic, v) }
func KafkaPartitionValue(v int32) attribute.KeyValue {
	return attribute.Int64(KafkaPartition, int64(v))
}
func KafkaOffsetValue(v int64) attribute.KeyValue      { return attribute.Int64(KafkaOffset, v) }
func ConsumerGroupValue(v string) attribute.KeyValue   { return attribute.String(ConsumerGroup, v) }
func MessagingSystemValue(v string) attribute.KeyValue { return attribute.String(MessagingSystem, v) }
func MessagingDestinationValue(v string) attribute.KeyValue {
	return attribute.String(MessagingDestination, v)
}

func MessagingDestinationKindValue(v string) attribute.KeyValue {
	return attribute.String(MessagingDestinationKind, v)
}

func EventIDValue(v string) attribute.KeyValue     { return attribute.String(EventID, v) }
func EventTypeValue(v string) attribute.KeyValue   { return attribute.String(EventType, v) }
func RetryCountValue(v int) attribute.KeyValue     { return attribute.Int(RetryCount, v) }
func BatchSizeValue(v int) attribute.KeyValue      { return attribute.Int(BatchSize, v) }
func BatchLimitValue(v int) attribute.KeyValue     { return attribute.Int(BatchLimit, v) }
func AggregateIDValue(v string) attribute.KeyValue { return attribute.String(AggregateID, v) }
func OutboxIDValue(v string) attribute.KeyValue    { return attribute.String(OutboxID, v) }
func StuckEventValue(v string) attribute.KeyValue  { return attribute.String(StuckEvent, v) }
func ReleasedEventCountValue(v int64) attribute.KeyValue {
	return attribute.Int64(ReleasedEventCount, v)
}

func DBSystemValue(v string) attribute.KeyValue { return attribute.String(DBSystem, v) }

func MarketEnabledValue(v bool) attribute.KeyValue { return attribute.Bool(MarketEnabled, v) }
func MarketDeletedValue(v bool) attribute.KeyValue { return attribute.Bool(MarketDeleted, v) }
func MarketBlockedValue(v bool) attribute.KeyValue { return attribute.Bool(MarketBlocked, v) }
func MarketBlockSyncFailedValue(v bool) attribute.KeyValue {
	return attribute.Bool(MarketBlockSyncFailed, v)
}

func MarketBlockSyncReasonValue(v string) attribute.KeyValue {
	return attribute.String(MarketBlockSyncReason, v)
}
func MarketsCountValue(v int) attribute.KeyValue { return attribute.Int(MarketsCount, v) }
