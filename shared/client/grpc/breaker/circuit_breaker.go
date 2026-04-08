package breaker

import (
	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
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
				// Инфраструктурные логи без контекста
				logger.Warn(nil, "circuit breaker is about to open",
					zap.String("name", name),
					zap.Uint32("consecutive_failures", counts.ConsecutiveFailures),
					zap.Uint32("total_failures", counts.TotalFailures),
					zap.Uint32("total_requests", counts.Requests),
				)
			}

			return shouldTrip
		},
		OnStateChange: func(breakerName string, from, to gobreaker.State) {
			metrics.CircuitBreakerStateChangesTotal.WithLabelValues(
				breakerName,
				from.String(),
				to.String(),
			).Inc()

			if to == gobreaker.StateOpen {
				metrics.CircuitBreakerOpenTotal.WithLabelValues(breakerName).Inc()
			}

			fields := []zap.Field{
				zap.String("name", breakerName),
				zap.String("from", from.String()),
				zap.String("to", to.String()),
			}

			switch to {
			case gobreaker.StateOpen:
				logger.Warn(nil, "circuit breaker is open", fields...)
			case gobreaker.StateHalfOpen:
				logger.Info(nil, "circuit breaker is half-open", fields...)
			case gobreaker.StateClosed:
				logger.Info(nil, "circuit breaker is closed", fields...)
			default:
				logger.Info(nil, "circuit breaker state changed", fields...)
			}
		},
	})
}

func isSuccessfulCall(err error) bool {
	if err == nil {
		return true
	}

	statusCode := errors.CodeFromError(err)

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
