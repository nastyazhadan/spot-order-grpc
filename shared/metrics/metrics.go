package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_grpc_requests_total",
			Help: "Throughput - total number of gRPC requests by service, method and status code",
		},
		[]string{"service", "method", "status"},
	)

	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "spotorder_grpc_request_duration_seconds",
			Help:    "gRPC handler duration in seconds — server-side latency and response time",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"service", "method"},
	)

	InFlightRequests = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotorder_in_flight_requests",
			Help: "Number of currently active gRPC requests",
		},
		[]string{"service"},
	)

	OrdersCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_orders_created_total",
			Help: "Total number of successfully created orders by service and market",
		},
		[]string{"service", "market_id"},
	)

	RateLimitRejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_rate_limit_rejected_total",
			Help: "Total number of requests rejected by the rate limiter",
		},
		[]string{"service", "method"},
	)

	CacheHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_cache_hits_total",
			Help: "Total number of cache hits",
		},
		[]string{"service", "operation"},
	)

	CacheMissesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_cache_misses_total",
			Help: "Total number of cache misses",
		},
		[]string{"service", "operation"},
	)

	CacheOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "spotorder_cache_operation_duration_seconds",
			Help:    "Latency of Redis cache operations",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1},
		},
		[]string{"service", "operation"},
	)

	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "spotorder_db_query_duration_seconds",
			Help:    "Latency of Postgres queries",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service", "operation"},
	)

	CircuitBreakerStateChangesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_circuit_breaker_state_changes_total",
			Help: "Total number of circuit breaker state transitions",
		},
		[]string{"name", "from", "to"},
	)

	CircuitBreakerOpenTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_circuit_breaker_open_total",
			Help: "Total number of times the circuit breaker opened",
		},
		[]string{"name"},
	)

	ShutdownsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_shutdowns_total",
			Help: "Total number of service shutdowns by service and reason",
		},
		[]string{"service", "reason"},
	)
)
