package kafka

import (
	"time"
)

const (
	SystemName      = "kafka"
	DestinationKind = "topic"
)

type Message struct {
	Headers        map[string][]byte
	Timestamp      time.Time
	BlockTimestamp time.Time

	Key       []byte
	Value     []byte
	Topic     string
	Partition int32
	Offset    int64
}

type HeadersCarrier map[string][]byte

func (c HeadersCarrier) Get(key string) string {
	if value, ok := c[key]; ok {
		return string(value)
	}
	return ""
}

func (c HeadersCarrier) Set(key, value string) {
	c[key] = []byte(value)
}

func (c HeadersCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}
