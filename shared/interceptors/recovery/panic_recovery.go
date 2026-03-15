package recovery

import (
	"context"
	"fmt"
	"runtime"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryServerInterceptor(
	ctx context.Context,
	request any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (response any, err error) {
	defer func() {
		if r := recover(); r != nil {
			buffer := make([]byte, 4096)
			count := runtime.Stack(buffer, false)

			zapLogger.Error(ctx, "panic recovered in gRPC handler",
				zap.String("panic", fmt.Sprintf("%v", r)),
				zap.ByteString("stack", buffer[:count]),
			)
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()

	return handler(ctx, request)
}
