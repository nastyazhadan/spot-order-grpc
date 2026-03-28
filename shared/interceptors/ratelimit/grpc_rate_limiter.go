package ratelimit

import (
	"context"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authProto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/auth/v1"
	orderProto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	spotProto "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

func OrderUnaryServerInterceptor(cfg config.OrderConfig, logger *zapLogger.Logger) grpc.UnaryServerInterceptor {
	return newUnaryServerInterceptor(map[string]int{
		orderProto.OrderService_CreateOrder_FullMethodName:    cfg.GRPCRateLimit.CreateOrder,
		orderProto.OrderService_GetOrderStatus_FullMethodName: cfg.GRPCRateLimit.GetOrderStatus,
		authProto.AuthService_RefreshToken_FullMethodName:     cfg.GRPCRateLimit.RefreshToken,
	}, cfg.Service.Name, logger)
}

func SpotUnaryServerInterceptor(cfg config.SpotConfig, logger *zapLogger.Logger) grpc.UnaryServerInterceptor {
	return newUnaryServerInterceptor(map[string]int{
		spotProto.SpotInstrumentService_ViewMarkets_FullMethodName:   cfg.GRPCRateLimit.ViewMarkets,
		spotProto.SpotInstrumentService_GetMarketByID_FullMethodName: cfg.GRPCRateLimit.GetMarketByID,
	}, cfg.Service.Name, logger)
}

func newUnaryServerInterceptor(
	methodsLimit map[string]int,
	serviceName string,
	logger *zapLogger.Logger,
) grpc.UnaryServerInterceptor {
	limiters := make(map[string]*rate.Limiter, len(methodsLimit))

	for method, rps := range methodsLimit {
		if rps <= 0 {
			continue
		}

		limiters[method] = rate.NewLimiter(rate.Limit(rps), rps)
	}

	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		limiter, ok := limiters[serverInfo.FullMethod]
		if !ok {
			return handler(ctx, request)
		}

		if !limiter.Allow() {
			logger.Warn(ctx, "grpc rate limit exceeded",
				zap.String("method", serverInfo.FullMethod),
			)

			metrics.RateLimitRejectedGRPCTotal.
				WithLabelValues(serviceName, serverInfo.FullMethod).Inc()

			return nil, status.Error(codes.ResourceExhausted, "too many requests")
		}

		return handler(ctx, request)
	}
}
