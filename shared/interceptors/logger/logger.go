package logger

import (
	"context"
	"path"
	"time"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func LoggerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		method := path.Base(info.FullMethod)
		startTime := time.Now()

		zapLogger.Info(ctx, "gRPC request started",
			zap.String("method", method),
		)

		response, err := handler(ctx, request)

		duration := time.Since(startTime)

		if err != nil {
			stat, _ := status.FromError(err)
			zapLogger.Error(ctx, "gRPC request failed",
				zap.String("method", method),
				zap.String("code", stat.Code().String()),
				zap.Duration("duration", duration),
				zap.Error(err),
			)
		} else {
			zapLogger.Info(ctx, "gRPC request completed",
				zap.String("method", method),
				zap.Duration("duration", duration),
			)
		}

		return response, err
	}
}
