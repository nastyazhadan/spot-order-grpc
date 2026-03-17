package breaker

import (
	"context"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
)

func New[T any](
	name string,
	cfg config.CircuitBreakerConfig,
) *gobreaker.CircuitBreaker[T] {
	return gobreaker.NewCircuitBreaker[T](gobreaker.Settings{
		Name:        name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			shouldTrip := counts.ConsecutiveFailures >= cfg.MaxFailures

			if shouldTrip {
				zapLogger.Warn(context.Background(), "circuit breaker is about to open",
					zap.String("name", name),
					zap.Uint32("consecutive_failures", counts.ConsecutiveFailures),
					zap.Uint32("total_failures", counts.TotalFailures),
					zap.Uint32("total_requests", counts.Requests),
				)
			}

			return shouldTrip
		},
		OnStateChange: func(breakerName string, from gobreaker.State, to gobreaker.State) {
			fields := []zap.Field{
				zap.String("name", breakerName),
				zap.String("from", from.String()),
				zap.String("to", to.String()),
			}

			switch to {
			case gobreaker.StateOpen:
				zapLogger.Warn(context.Background(), "circuit breaker is open", fields...)
			case gobreaker.StateHalfOpen:
				zapLogger.Info(context.Background(), "circuit breaker is half-open", fields...)
			case gobreaker.StateClosed:
				zapLogger.Info(context.Background(), "circuit breaker is closed", fields...)
			default:
				zapLogger.Info(context.Background(), "circuit breaker state changed", fields...)
			}
		},
	})
}
