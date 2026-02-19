package recovery

import (
	"context"
	"fmt"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Unary(
	ctx context.Context,
	request interface{},
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (response interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			zapLogger.Error(ctx, "panic recovered in gRPC handler",
				zap.String("panic", fmt.Sprintf("%v", r)),
			)

			err = status.Errorf(codes.Internal, "internal error")
		}
	}()

	return handler(ctx, request)
}
