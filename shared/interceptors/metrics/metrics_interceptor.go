package metrics

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

func UnaryServerInterceptor(serviceName string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		start := time.Now()
		metrics.InFlightRequests.WithLabelValues(serviceName, serverInfo.FullMethod).Inc()

		defer func() {
			metrics.InFlightRequests.WithLabelValues(serviceName, serverInfo.FullMethod).Dec()

			code := status.Code(err).String()

			// Ловим panic для того, чтобы не потерять метрики.
			// После записи метрик обязательно re-panic, чтобы внешний recovery interceptor
			// завершил обработку запроса как gRPC Internal
			if r := recover(); r != nil {
				code = codes.Internal.String()

				metrics.RequestsTotal.WithLabelValues(
					serviceName,
					serverInfo.FullMethod,
					code,
				).Inc()

				metrics.RequestDuration.WithLabelValues(
					serviceName,
					serverInfo.FullMethod,
				).Observe(time.Since(start).Seconds())

				panic(r)
			}

			metrics.RequestsTotal.WithLabelValues(
				serviceName,
				serverInfo.FullMethod,
				code,
			).Inc()

			metrics.RequestDuration.WithLabelValues(
				serviceName,
				serverInfo.FullMethod,
			).Observe(time.Since(start).Seconds())
		}()

		resp, err = handler(ctx, request)
		return resp, err
	}
}
