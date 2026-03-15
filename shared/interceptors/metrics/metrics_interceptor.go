package metrics

import (
	"context"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryServerInterceptor(serviceName string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, serverInfo *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		metrics.InFlightRequests.WithLabelValues(serviceName).Inc()
		defer metrics.InFlightRequests.WithLabelValues(serviceName).Dec()

		start := time.Now()
		response, err := handler(ctx, request)
		duration := time.Since(start)

		metrics.RequestDuration.WithLabelValues(serviceName,
			serverInfo.FullMethod).Observe(duration.Seconds())

		statusCode := codes.OK
		if err != nil {
			if stat, ok := status.FromError(err); ok {
				statusCode = stat.Code()
			} else {
				statusCode = codes.Internal
			}
		}

		metrics.RequestsTotal.WithLabelValues(serviceName,
			serverInfo.FullMethod, statusCode.String()).Inc()

		return response, err
	}
}
