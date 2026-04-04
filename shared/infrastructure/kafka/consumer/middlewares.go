package consumer

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/go-faster/errors"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

func PanicRecoveryMiddleware(
	logger *zapLogger.Logger,
) Middleware {
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

func RetryMiddleware(
	maxRetries int,
	backoff time.Duration,
	logger *zapLogger.Logger,
) Middleware {
	if maxRetries < 0 {
		maxRetries = 0
	}

	return func(next MessageHandler) MessageHandler {
		return func(ctx context.Context, message kafka.Message) error {
			for attempt := range maxRetries + 1 {
				if err := next(ctx, message); err != nil {
					if IsNonRetryableError(err) {
						return err
					}

					if attempt < maxRetries {
						sleep := backoff * time.Duration(attempt+1)

						logger.Warn(ctx, "Kafka message processing failed, retrying",
							zap.String("topic", message.Topic),
							zap.Int("attempt", attempt+1),
							zap.Int("max_retries", maxRetries),
							zap.Duration("retry_after", sleep),
							zap.Error(err),
						)

						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(sleep):
							continue
						}
					}
					return RetryExhaustedError{
						Err:        err,
						RetryCount: maxRetries,
					}
				}
				return nil
			}
			return nil
		}
	}
}

func DLQMiddleware(
	dlqPublisher DLQPublisher,
	maxMessageBytes int,
	logger *zapLogger.Logger,
) Middleware {
	return func(next MessageHandler) MessageHandler {
		return func(ctx context.Context, message kafka.Message) error {
			err := next(ctx, message)
			if err == nil {
				return nil
			}

			if IsControlFlowError(err) {
				return err
			}

			retryCount := 0
			var exhaustedErr RetryExhaustedError
			if errors.As(err, &exhaustedErr) {
				retryCount = exhaustedErr.RetryCount
				err = exhaustedErr.Err
			}

			if IsControlFlowError(err) {
				return err
			}

			return sendToDLQ(ctx, message, dlqPublisher, logger, err, retryCount, maxMessageBytes)
		}
	}
}
