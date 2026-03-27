package consumer

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

var (
	ErrMessageHandledByDLQ = errors.New("message handled by dlq")
	ErrStopConsumerSession = errors.New("stop consumer session")
)

type DLQPublisher interface {
	SendMessage(ctx context.Context, msg kafka.Message) error
}

type MessageHandler func(ctx context.Context, msg kafka.Message) error

type Middleware func(next MessageHandler) MessageHandler

type consumer struct {
	group       sarama.ConsumerGroup
	topics      []string
	serviceName string
	logger      *zapLogger.Logger
	middlewares []Middleware
}

func New(
	group sarama.ConsumerGroup,
	topics []string,
	name string,
	logger *zapLogger.Logger,
	middlewares ...Middleware,
) *consumer {
	middlewares = append([]Middleware{PanicRecoveryMiddleware(logger)}, middlewares...)

	return &consumer{
		group:       group,
		topics:      topics,
		serviceName: name,
		logger:      logger,
		middlewares: middlewares,
	}
}

func (c *consumer) Consume(ctx context.Context, handler MessageHandler) error {
	wrappedHandler := applyMiddlewares(handler, c.middlewares...)
	newHandler := NewGroupHandler(wrappedHandler, c.serviceName, c.logger)

	for {
		if err := c.group.Consume(ctx, c.topics, newHandler); err != nil {
			if errors.Is(err, sarama.ErrClosedConsumerGroup) {
				return nil
			}

			if errors.Is(err, ErrStopConsumerSession) {
				c.logger.Warn(ctx, "Kafka consumer session stopped, restarting session")
				continue
			}

			c.logger.Error(ctx, "Kafka consume error", zap.Error(err))
			return err
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		c.logger.Info(ctx, "Kafka consumer group rebalancing...")
	}
}

func applyMiddlewares(handler MessageHandler, middlewares ...Middleware) MessageHandler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}

	return handler
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
		return func(ctx context.Context, msg kafka.Message) error {
			for attempt := range maxRetries + 1 {
				if err := next(ctx, msg); err != nil {
					if attempt < maxRetries {
						sleep := backoff * time.Duration(attempt+1)

						logger.Warn(ctx, "Kafka message processing failed, retrying",
							zap.String("topic", msg.Topic),
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
	serviceName string,
	dlqPublisher DLQPublisher,
	logger *zapLogger.Logger,
) Middleware {
	return func(next MessageHandler) MessageHandler {
		return func(ctx context.Context, msg kafka.Message) error {
			err := next(ctx, msg)
			if err == nil {
				return nil
			}

			retryCount := 0
			var exhaustedErr RetryExhaustedError
			if errors.As(err, &exhaustedErr) {
				retryCount = exhaustedErr.RetryCount
				err = exhaustedErr.Err
			}

			return sendToDLQ(ctx, msg, serviceName, dlqPublisher, logger, err, retryCount)
		}
	}
}

func sendToDLQ(
	ctx context.Context,
	msg kafka.Message,
	serviceName string,
	dlqPublisher DLQPublisher,
	logger *zapLogger.Logger,
	lastErr error,
	retryCount int,
) error {
	if dlqPublisher == nil {
		logger.Error(ctx, "Message processing failed (no DLQ configured)",
			zap.String("topic", msg.Topic),
			zap.Int("retry_count", retryCount),
			zap.Error(lastErr),
		)
		return lastErr
	}

	dlqMessage := buildDLQMessage(msg, lastErr, retryCount)

	if dlqError := dlqPublisher.SendMessage(ctx, dlqMessage); dlqError != nil {
		logger.Error(ctx, "Failed to send message to DLQ",
			zap.String("topic", msg.Topic),
			zap.Error(dlqError),
		)
		return dlqError
	}

	metrics.KafkaMessagesConsumedTotal.WithLabelValues(serviceName, msg.Topic, "dlq").Inc()

	logger.Error(ctx, "Message sent to DLQ after all retries",
		zap.String("topic", msg.Topic),
		zap.Int("retry_count", retryCount),
		zap.Error(lastErr),
	)

	return ErrMessageHandledByDLQ
}

func buildDLQMessage(msg kafka.Message, lastErr error, retryCount int) kafka.Message {
	headers := make(map[string][]byte, len(msg.Headers)+3)

	for key, value := range msg.Headers {
		copied := make([]byte, len(value))
		copy(copied, value)
		headers[key] = copied
	}

	headers["dlq-original-topic"] = []byte(msg.Topic)
	headers["dlq-error"] = []byte(lastErr.Error())
	headers["dlq-retry-count"] = []byte(strconv.Itoa(retryCount))

	return kafka.Message{
		Headers:        headers,
		Timestamp:      msg.Timestamp,
		BlockTimestamp: msg.BlockTimestamp,
		Key:            msg.Key,
		Value:          msg.Value,
		Partition:      msg.Partition,
		Offset:         msg.Offset,
	}
}
