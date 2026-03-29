package metrics

import (
	"context"
	"errors"
)

const (
	ShutdownReasonGraceful = "graceful"
	ShutdownReasonTimeout  = "timeout"
	ShutdownReasonError    = "error"
)

func RecordShutdown(serviceName, reason string) {
	ShutdownsTotal.WithLabelValues(serviceName, reason).Inc()
}

func ShutdownReasonFromError(err error) string {
	switch {
	case err == nil:
		return ShutdownReasonGraceful
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return ShutdownReasonTimeout
	default:
		return ShutdownReasonError
	}
}
