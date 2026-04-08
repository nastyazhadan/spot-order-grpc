package consumer

import (
	"context"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

const (
	dlqExtraHeaders  = 8
	dlqErrorMaxBytes = 2 * 1024
)

var dlqEssentialHeaders = map[string]struct{}{
	"traceparent":               {},
	"dlq-original-topic":        {},
	"dlq-original-partition":    {},
	"dlq-original-offset":       {},
	"dlq-attempts-total":        {},
	"dlq-error":                 {},
	"dlq-payload-intact":        {},
	"dlq-metadata-trimmed":      {},
	"dlq-original-message-size": {},
	"dlq-original-payload-size": {},
}

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
	lastErr error,
	retryCount int,
	maxMessageBytes int,
	logger *zapLogger.Logger,
) error {
	if dlqPublisher == nil {
		logger.Error(ctx, "Message processing failed (no DLQ configured)",
			zap.String("topic", message.Topic),
			zap.Int("retry_count", retryCount),
			zap.Error(lastErr),
		)
		return lastErr
	}

	dlqMessage := buildDLQMessage(ctx, message, lastErr, retryCount, maxMessageBytes, logger)

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
	ctx context.Context,
	message kafka.Message,
	lastError error,
	retryCount int,
	maxMessageBytes int,
	logger *zapLogger.Logger,
) kafka.Message {
	headers := buildDLQHeaders(message, lastError, retryCount)
	dlqMessage := newDLQMessage(message, headers)

	if !shouldTruncate(dlqMessage, maxMessageBytes) {
		return dlqMessage
	}

	dlqMessage.Headers["dlq-metadata-trimmed"] = []byte("true")
	dlqMessage.Headers["dlq-original-message-size"] = []byte(strconv.Itoa(messageSize(message)))
	dlqMessage.Headers["dlq-original-payload-size"] = []byte(strconv.Itoa(len(message.Value)))

	trimDLQHeadersToEssential(&dlqMessage)

	if shouldTruncate(dlqMessage, maxMessageBytes) {
		dlqMessage.Headers["dlq-error"] = truncateUTF8Bytes(dlqMessage.Headers["dlq-error"], 256)
	}

	if shouldTruncate(dlqMessage, maxMessageBytes) {
		delete(dlqMessage.Headers, "dlq-error")
	}

	if shouldTruncate(dlqMessage, maxMessageBytes) {
		logger.Warn(ctx, "DLQ message still exceeds max size after metadata trimming",
			zap.String("topic", message.Topic),
			zap.Int("message_size", messageSize(dlqMessage)),
			zap.Int("max_message_bytes", maxMessageBytes),
		)
	}

	return dlqMessage
}

func buildDLQHeaders(
	message kafka.Message,
	lastError error,
	retryCount int,
) map[string][]byte {
	headers := make(map[string][]byte, len(message.Headers)+dlqExtraHeaders)

	// Значения headers не копируем отдельно, потому что дальше не изменяем их содержимое
	for key, value := range message.Headers {
		headers[key] = value
	}

	headers["dlq-original-topic"] = []byte(message.Topic)
	headers["dlq-original-partition"] = []byte(strconv.FormatInt(int64(message.Partition), 10))
	headers["dlq-original-offset"] = []byte(strconv.FormatInt(message.Offset, 10))
	headers["dlq-attempts-total"] = []byte(strconv.Itoa(retryCount))
	headers["dlq-error"] = truncateUTF8Bytes([]byte(lastError.Error()), dlqErrorMaxBytes)
	// Явный флаг для DLQ
	headers["dlq-payload-intact"] = []byte("true")

	return headers
}

func newDLQMessage(message kafka.Message, headers map[string][]byte) kafka.Message {
	return kafka.Message{
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

func trimDLQHeadersToEssential(message *kafka.Message) {
	result := make(map[string][]byte, len(dlqEssentialHeaders))

	for key, value := range message.Headers {
		if _, ok := dlqEssentialHeaders[key]; ok {
			result[key] = value
		}
	}

	message.Headers = result
}

func truncateUTF8Bytes(data []byte, limit int) []byte {
	if limit <= 0 {
		return nil
	}

	if len(data) <= limit {
		return data
	}

	truncated := data[:limit]
	for len(truncated) > 0 && !utf8.Valid(truncated) {
		truncated = truncated[:len(truncated)-1]
	}

	return truncated
}

func messageSize(message kafka.Message) int {
	size := len(message.Key) + len(message.Value)

	for key, value := range message.Headers {
		size += len(key) + len(value)
	}

	return size
}
