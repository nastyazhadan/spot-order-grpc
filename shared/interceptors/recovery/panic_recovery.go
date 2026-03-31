package recovery

import (
	"context"
	"fmt"
	"runtime/debug"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

func UnaryServerInterceptor(logger *zapLogger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (response any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error(ctx, "panic recovered in gRPC handler",
					zap.String("panic", fmt.Sprintf("%v", r)),
					zap.ByteString("stack", debug.Stack()),
				)

				err = status.Error(codes.Internal, "internal error")
			}
		}()

		return handler(ctx, request)
	}
}
