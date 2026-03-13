package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_grpc_requests_total",
			Help: "Total number of gRPC requests by service, method and status code",
		},
		[]string{"service", "method", "status"},
	)

	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "spotorder_grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"service", "method"},
	)

	ActiveConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotorder_grpc_active_connections",
			Help: "Number of currently in-flight gRPC requests",
		},
		[]string{"service"},
	)

	OrdersCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_orders_created_total",
			Help: "Total number of successfully created orders",
		},
		[]string{"service"},
	)

	RateLimitRejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_rate_limit_rejected_total",
			Help: "Total number of requests rejected by the rate limiter",
		},
		[]string{"service", "method"},
	)

	ShutdownsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotorder_shutdowns_total",
			Help: "Total number of service shutdowns by service and reason",
		},
		[]string{"service", "reason"},
	)
)
