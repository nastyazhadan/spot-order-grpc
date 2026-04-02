package consumer

import (
	"context"
	"errors"
	"strconv"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

const dlqExtraHeaders = 3

var (
	ErrMessageHandledByDLQ    = errors.New("message handled by dlq")
	ErrRestartConsumerSession = errors.New("restart consumer session")
	ErrSkipMessage            = errors.New("skip message")
)

type DLQPublisher interface {
	SendMessage(ctx context.Context, message kafka.Message) error
}

type MessageHandler func(ctx context.Context, message kafka.Message) error

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
	newGroupHandler := newHandler(wrappedHandler, c.serviceName, c.logger)

	// Здесь цикл для перезапуска consumer group session (например, после rebalance
	// или запроса на restart session). В lifecycle.go находится внешний цикл для
	// рестарта всего consumer при фатальных ошибках
	for {
		err := c.group.Consume(ctx, c.topics, newGroupHandler)
		if err != nil {
			switch {
			case errors.Is(err, sarama.ErrClosedConsumerGroup):
				return nil
			case errors.Is(err, ErrRestartConsumerSession):
				c.logger.Warn(ctx, "Kafka consumer session stopped, restarting session")
				continue
			default:
				c.logger.Error(ctx, "Kafka consume error", zap.Error(err))
				return err
			}
		}

		if ctx.Err() != nil {
			return nil
		}

		c.logger.Info(ctx, "Kafka consumer session ended, restarting")
	}
}

func applyMiddlewares(handler MessageHandler, middlewares ...Middleware) MessageHandler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}

	return handler
}

func sendToDLQ(
	ctx context.Context,
	message kafka.Message,
	dlqPublisher DLQPublisher,
	logger *zapLogger.Logger,
	lastErr error,
	retryCount int,
	maxMessageBytes int,
) error {
	if dlqPublisher == nil {
		logger.Error(ctx, "Message processing failed (no DLQ configured)",
			zap.String("topic", message.Topic),
			zap.Int("retry_count", retryCount),
			zap.Error(lastErr),
		)
		return lastErr
	}

	dlqMessage := buildDLQMessage(message, lastErr, retryCount, maxMessageBytes)

	if dlqError := dlqPublisher.SendMessage(ctx, dlqMessage); dlqError != nil {
		logger.Error(ctx, "Failed to send message to DLQ",
			zap.String("topic", message.Topic),
			zap.Error(dlqError),
		)
		return dlqError
	}

	logger.Error(ctx, "Message sent to DLQ after all retries",
		zap.String("topic", message.Topic),
		zap.Int("retry_count", retryCount),
		zap.Error(lastErr),
	)

	return ErrMessageHandledByDLQ
}

func buildDLQMessage(
	message kafka.Message,
	lastError error,
	retryCount int,
	maxBytes int,
) kafka.Message {
	headers := buildDLQHeaders(message, lastError, retryCount)

	dlqMessage := newDLQMessage(message, headers)
	if !shouldTruncate(dlqMessage, maxBytes) {
		return dlqMessage
	}

	originalSize := messageSize(dlqMessage)

	headers["dlq-truncated"] = []byte("true")
	headers["dlq-original-size"] = []byte(strconv.Itoa(originalSize))

	// Пробуем уменьшить текст за счет ошибки dlq-error
	truncateDLQError(&dlqMessage, lastError.Error(), maxBytes)
	// Если текст все еще больше лимита, режем payload
	truncateDLQValue(&dlqMessage, maxBytes)

	currentSize := messageSize(dlqMessage)
	headers["dlq-final-size"] = []byte(strconv.Itoa(currentSize))

	return dlqMessage
}

func buildDLQHeaders(
	message kafka.Message,
	lastErr error,
	retryCount int,
) map[string][]byte {
	headers := make(map[string][]byte, len(message.Headers)+dlqExtraHeaders)

	// Если header values неизменяемые - копировать не надо
	for key, value := range message.Headers {
		headers[key] = value
	}

	headers["dlq-original-topic"] = []byte(message.Topic)
	headers["dlq-retry-count"] = []byte(strconv.Itoa(retryCount))
	headers["dlq-error"] = []byte(lastErr.Error())

	return headers
}

func newDLQMessage(message kafka.Message, headers map[string][]byte) kafka.Message {
	return kafka.Message{
		Topic:          message.Topic,
		Headers:        headers,
		Timestamp:      message.Timestamp,
		BlockTimestamp: message.BlockTimestamp,
		Key:            message.Key,
		Value:          message.Value,
		Partition:      message.Partition,
		Offset:         message.Offset,
	}
}

func shouldTruncate(message kafka.Message, maxBytes int) bool {
	return maxBytes > 0 && messageSize(message) > maxBytes
}

func truncateDLQError(message *kafka.Message, errorText string, maxBytes int) {
	sizeWithoutError := messageSize(kafka.Message{
		Topic:          message.Topic,
		Headers:        buildHeadersWithout(message.Headers, "dlq-error"),
		Timestamp:      message.Timestamp,
		BlockTimestamp: message.BlockTimestamp,
		Key:            message.Key,
		Value:          message.Value,
		Partition:      message.Partition,
		Offset:         message.Offset,
	})

	availableForError := maxBytes - sizeWithoutError
	if availableForError < 0 {
		availableForError = 0
	}

	message.Headers["dlq-error"] = truncateBytes([]byte(errorText), availableForError)
}

func buildHeadersWithout(source map[string][]byte, exclude string) map[string][]byte {
	result := make(map[string][]byte, len(source))
	for key, value := range source {
		if key == exclude {
			continue
		}
		result[key] = value
	}
	return result
}

func truncateDLQValue(message *kafka.Message, maxBytes int) {
	currentSize := messageSize(*message)
	if currentSize <= maxBytes {
		return
	}

	sizeWithoutValue := currentSize - len(message.Value)
	availableForValue := maxBytes - sizeWithoutValue
	if availableForValue < 0 {
		availableForValue = 0
	}

	message.Value = truncateBytes(message.Value, availableForValue)
}

func messageSize(message kafka.Message) int {
	size := len(message.Key) + len(message.Value)

	for key, value := range message.Headers {
		size += len(key) + len(value)
	}

	return size
}

func truncateBytes(data []byte, limit int) []byte {
	if limit <= 0 {
		return nil
	}
	if len(data) <= limit {
		return data
	}
	return data[:limit]
}
