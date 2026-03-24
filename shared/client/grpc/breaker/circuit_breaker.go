package breaker

import (
	"context"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

func New[T any](
	name string,
	cfg config.CircuitBreakerConfig,
	logger *zapLogger.Logger,
) *gobreaker.CircuitBreaker[T] {
	return gobreaker.NewCircuitBreaker[T](gobreaker.Settings{
		Name:        name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			shouldTrip := counts.ConsecutiveFailures >= cfg.MaxFailures

			if shouldTrip {
				metrics.CircuitBreakerOpenTotal.WithLabelValues(name).Inc()

				logger.Warn(context.Background(), "circuit breaker is about to open",
					zap.String("name", name),
					zap.Uint32("consecutive_failures", counts.ConsecutiveFailures),
					zap.Uint32("total_failures", counts.TotalFailures),
					zap.Uint32("total_requests", counts.Requests),
				)
			}

			return shouldTrip
		},
		OnStateChange: func(breakerName string, from gobreaker.State, to gobreaker.State) {
			metrics.CircuitBreakerStateChangesTotal.WithLabelValues(
				breakerName,
				from.String(),
				to.String(),
			).Inc()

			fields := []zap.Field{
				zap.String("name", breakerName),
				zap.String("from", from.String()),
				zap.String("to", to.String()),
			}

			switch to {
			case gobreaker.StateOpen:
				logger.Warn(context.Background(), "circuit breaker is open", fields...)
			case gobreaker.StateHalfOpen:
				logger.Info(context.Background(), "circuit breaker is half-open", fields...)
			case gobreaker.StateClosed:
				logger.Info(context.Background(), "circuit breaker is closed", fields...)
			default:
				logger.Info(context.Background(), "circuit breaker state changed", fields...)
			}
		},
	})
}
