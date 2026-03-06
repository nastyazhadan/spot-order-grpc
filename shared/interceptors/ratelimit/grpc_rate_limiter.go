package ratelimit

import (
	"context"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func RateLimiter(rps int) grpc.UnaryServerInterceptor {
	limiter := rate.NewLimiter(rate.Limit(rps), rps)

	return func(
		ctx context.Context,
		request interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if !limiter.Allow() {
			address := "unknown"
			if p, ok := peer.FromContext(ctx); ok {
				address = p.Addr.String()
			}

			zapLogger.Warn(ctx, "global rate limit exceeded",
				zap.String("method", info.FullMethod),
				zap.String("peer", address),
			)

			return nil, status.Error(codes.ResourceExhausted, "too many requests")
		}

		return handler(ctx, request)
	}
}
