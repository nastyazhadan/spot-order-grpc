package consumer

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"time"

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
	jitter float64,
	logger *zapLogger.Logger,
) Middleware {
	if maxRetries < 0 {
		maxRetries = 0
	}
	if jitter < 0 {
		jitter = 0
	}
	if jitter > 1 {
		jitter = 1
	}

	return func(next MessageHandler) MessageHandler {
		return func(ctx context.Context, message kafka.Message) error {
			for attempt := range maxRetries + 1 {
				if err := next(ctx, message); err != nil {
					if IsNonRetryableError(err) {
						return err
					}

					if attempt < maxRetries {
						sleep := retryDelay(backoff, attempt, jitter)

						logger.Warn(ctx, "Kafka message processing failed, retrying",
							zap.String("topic", message.Topic),
							zap.Int("attempt", attempt+1),
							zap.Int("max_retries", maxRetries),
							zap.Duration("retry_after", sleep),
							zap.Float64("retry_jitter", jitter),
							zap.Error(err),
						)

						timer := time.NewTimer(sleep)

						select {
						case <-ctx.Done():
							if !timer.Stop() {
								select {
								case <-timer.C:
								default:
								}
							}
							return ctx.Err()
						case <-timer.C:
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

func retryDelay(backoff time.Duration, attempt int, jitter float64) time.Duration {
	base := backoff * time.Duration(attempt+1)

	if base <= 0 || jitter <= 0 {
		return base
	}

	delta := time.Duration(rand.Float64() * jitter * float64(base))
	if rand.IntN(2) == 0 {
		return base - delta
	}

	return base + delta
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
				retryCount = exhaustedErr.RetryCount + 1
				err = exhaustedErr.Err
			}

			if IsControlFlowError(err) {
				return err
			}

			return sendToDLQ(ctx, message, dlqPublisher, err, retryCount, maxMessageBytes, logger)
		}
	}
}
