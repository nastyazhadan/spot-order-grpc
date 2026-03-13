package rate_limit

import (
	"context"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func UnaryServerInterceptor(rps int, serviceName string) grpc.UnaryServerInterceptor {
	limiter := rate.NewLimiter(rate.Limit(rps), rps)

	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if !limiter.Allow() {
			address := "unknown"
			if p, ok := peer.FromContext(ctx); ok {
				address = p.Addr.String()
			}

			zapLogger.Warn(ctx, "global rate limit exceeded",
				zap.String("method", info.FullMethod),
				zap.String("peer", address),
			)

			metrics.RateLimitRejectedTotal.WithLabelValues(serviceName, info.FullMethod).Inc()

			return nil, status.Error(codes.ResourceExhausted, "too many requests")
		}

		return handler(ctx, request)
	}
}
