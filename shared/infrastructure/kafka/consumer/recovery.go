package consumer

import (
	"context"
	"fmt"
	"runtime/debug"

	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

func PanicRecoveryMiddleware(logger *zapLogger.Logger) Middleware {
	return func(next MessageHandler) MessageHandler {
		return func(ctx context.Context, msg kafka.Message) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic recovered in kafka handler: %v", r)

					logger.Error(ctx, "panic recovered in Kafka handler",
						zap.String("topic", msg.Topic),
						zap.Int32("partition", msg.Partition),
						zap.Int64("offset", msg.Offset),
						zap.Any("panic", r),
						zap.ByteString("stack", debug.Stack()),
					)
				}
			}()

			return next(ctx, msg)
		}
	}
}
