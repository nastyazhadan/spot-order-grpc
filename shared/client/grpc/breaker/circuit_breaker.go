package breaker

import (
	"context"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
		Name:         name,
		MaxRequests:  cfg.MaxRequests,
		Interval:     cfg.Interval,
		Timeout:      cfg.Timeout,
		IsSuccessful: isSuccessfulCall,
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

func isSuccessfulCall(err error) bool {
	if err == nil {
		return true
	}

	statusCode := status.Code(err)
	switch statusCode {
	case codes.Canceled,
		codes.InvalidArgument,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.Unauthenticated,
		codes.FailedPrecondition,
		codes.ResourceExhausted,
		codes.OutOfRange:
		return true
	default:
		return false
	}
}
