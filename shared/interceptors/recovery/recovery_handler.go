package recovery

import (
	"context"
	"fmt"
	"runtime/debug"

	"go.uber.org/zap"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

func PanicRecoveryHandler(
	ctx context.Context,
	logger *zapLogger.Logger,
	component string,
	function func() error,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, component+" panicked",
				zap.Any("panic", r),
				zap.ByteString("stack", debug.Stack()),
			)
			err = fmt.Errorf("%s panic: %v", component, r)
		}
	}()

	return function()
}
