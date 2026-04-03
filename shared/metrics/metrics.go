package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/trace"
)

var (
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_requests_total",
			Help: "Throughput - total number of gRPC requests by service, method and status code",
		},
		[]string{"service", "method", "status"},
	)

	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_request_duration_seconds",
			Help:    "gRPC handler duration in seconds — server-side latency and response time",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"service", "method"},
	)

	InFlightRequests = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grpc_server_in_flight_requests",
			Help: "Current number of in-flight gRPC requests",
		},
		[]string{"service", "method"},
	)

	OrdersCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_orders_created_total",
			Help: "Total number of successfully created orders by service and market",
		},
		[]string{"service", "market_id"},
	)

	RateLimitRejectedGRPCTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_rate_limit_rejected_grpc_total",
			Help: "Total number of requests rejected by gRPC transport rate limiter",
		},
		[]string{"service", "method"},
	)

	RateLimitRejectedBusinessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_rate_limit_rejected_business_total",
			Help: "Total number of requests rejected by business rate limiter",
		},
		[]string{"service", "operation"},
	)

	CacheHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_cache_hits_total",
			Help: "Total number of cache hits",
		},
		[]string{"service", "operation"},
	)

	CacheMissesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_cache_misses_total",
			Help: "Total number of cache misses",
		},
		[]string{"service", "operation"},
	)

	CacheOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_cache_operation_duration_seconds",
			Help:    "Latency of Redis cache operations",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1},
		},
		[]string{"service", "operation"},
	)

	CacheInvalidationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_cache_invalidations_total",
			Help: "Total number of cache invalidations",
		},
		[]string{"service", "reason", "role", "result"},
	)

	CacheFallbacksTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_cache_fallbacks_total",
			Help: "Total number of fallbacks after cache lookup failure",
		},
		[]string{"service", "operation", "reason"},
	)

	CacheWarmupsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_cache_warmups_total",
			Help: "Total number of cache warmup attempts",
		},
		[]string{"service", "operation", "role", "result"},
	)

	MarketBlockStateSyncTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_market_block_state_sync_total",
			Help: "Total number of market block state sync attempts",
		},
		[]string{"service", "reason", "blocked", "result", "updated"},
	)

	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_db_query_duration_seconds",
			Help:    "Latency of Postgres queries",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service", "operation"},
	)

	CircuitBreakerStateChangesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_circuit_breaker_state_changes_total",
			Help: "Total number of circuit breaker state transitions",
		},
		[]string{"name", "from", "to"},
	)

	CircuitBreakerOpenTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_circuit_breaker_open_total",
			Help: "Total number of times the circuit breaker opened",
		},
		[]string{"name"},
	)

	ShutdownsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_shutdowns_total",
			Help: "Total number of service shutdowns by service and reason",
		},
		[]string{"service", "reason"},
	)

	KafkaMessagesPublishedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_kafka_messages_published_total",
			Help: "Total number of Kafka messages successfully published",
		},
		[]string{"service", "topic"},
	)

	KafkaPublishErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_kafka_publish_errors_total",
			Help: "Total number of Kafka publish errors",
		},
		[]string{"service", "topic"},
	)

	KafkaPublishDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_kafka_publish_duration_seconds",
			Help:    "Latency of Kafka message publish operations",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service", "topic"},
	)

	KafkaMessagesConsumedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_kafka_messages_consumed_total",
			Help: "Total number of Kafka messages consumed",
		},
		[]string{"service", "topic", "result"},
	)

	KafkaConsumeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_kafka_consume_duration_seconds",
			Help:    "Latency of Kafka message processing",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"service", "topic"},
	)

	OutboxEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_outbox_events_total",
			Help: "Total number of outbox events processed by the worker",
		},
		[]string{"service", "event_type", "result"},
	)

	OutboxWorkerDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_server_outbox_worker_iteration_duration_seconds",
			Help:    "Duration of a single outbox worker poll iteration",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service"},
	)
)

func ObserveWithTrace(ctx context.Context, wrap prometheus.Observer, time float64) {
	if exemplar, ok := wrap.(prometheus.ExemplarObserver); ok {
		traceID := trace.SpanFromContext(ctx).SpanContext().TraceID().String()
		if traceID != "00000000000000000000000000000000" {
			exemplar.ObserveWithExemplar(time, prometheus.Labels{"traceID": traceID})
			return
		}
	}
	wrap.Observe(time)
}
