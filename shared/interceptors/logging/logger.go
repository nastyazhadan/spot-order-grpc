package logging

import (
	"context"
	"path"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

func UnaryServerInterceptor(logger *zapLogger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		method := path.Base(serverInfo.FullMethod)
		startTime := time.Now()

		logger.Info(ctx, "gRPC request started",
			zap.String("method", method),
		)

		response, err := handler(ctx, request)

		duration := time.Since(startTime)

		if err != nil {
			code := errors.CodeFromError(err)

			fields := []zap.Field{
				zap.String("method", method),
				zap.String("code", code.String()),
				zap.Duration("duration", duration),
			}

			switch code {
			case codes.NotFound,
				codes.AlreadyExists,
				codes.PermissionDenied,
				codes.ResourceExhausted,
				codes.InvalidArgument,
				codes.Unauthenticated,
				codes.FailedPrecondition,
				codes.Canceled,
				codes.DeadlineExceeded:
				logger.Warn(ctx, "gRPC request failed", fields...)
			default:
				logger.Error(ctx, "gRPC request failed", fields...)
			}
		}

		return response, err
	}
}
