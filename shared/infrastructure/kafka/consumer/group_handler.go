package consumer

import (
	"context"
	"errors"
	"time"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

const instrumentationName = "spot-order-grpc/kafka/consumer"

type groupHandler struct {
	handler     MessageHandler
	serviceName string
	logger      *zapLogger.Logger
}

func newHandler(handler MessageHandler, name string, logger *zapLogger.Logger) *groupHandler {
	return &groupHandler{
		handler:     handler,
		serviceName: name,
		logger:      logger,
	}
}

func (g *groupHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

func (g *groupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (g *groupHandler) ConsumeClaim(
	session sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
) error {
	for {
		select {
		case message, ok := <-claim.Messages():
			if !ok {
				g.logger.Info(session.Context(), "Kafka message channel closed")
				return nil
			}

			if err := g.handleMessage(session, message); err != nil {
				return err
			}

		case <-session.Context().Done():
			g.logger.Info(session.Context(), "Kafka session context done")
			return nil
		}
	}
}

func (g *groupHandler) handleMessage(
	session sarama.ConsumerGroupSession,
	message *sarama.ConsumerMessage,
) error {
	ctx, span, msg := g.startMessageProcessing(session, message)
	defer span.End()

	start := time.Now()
	err := g.handler(ctx, msg)
	elapsed := time.Since(start).Seconds()

	metrics.ObserveWithTrace(
		ctx,
		metrics.KafkaConsumeDuration.WithLabelValues(g.serviceName, message.Topic),
		elapsed,
	)

	return g.handleMessageResult(ctx, span, session, message, err)
}

func (g *groupHandler) startMessageProcessing(
	session sarama.ConsumerGroupSession,
	message *sarama.ConsumerMessage,
) (context.Context, trace.Span, kafka.Message) {
	headers := extractHeaders(message.Headers)
	carrier := kafka.HeadersCarrier(headers)

	ctx := otel.GetTextMapPropagator().Extract(session.Context(), carrier)

	ctx, span := otel.Tracer(instrumentationName).Start(
		ctx,
		"kafka.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attributes.MessagingSystemValue(kafka.SystemName),
			attributes.KafkaTopicValue(message.Topic),
			attributes.MessagingDestinationKindValue(kafka.DestinationKind),
			attributes.KafkaPartitionValue(message.Partition),
			attributes.KafkaOffsetValue(message.Offset),
		),
	)

	msg := kafka.Message{
		Key:            message.Key,
		Value:          message.Value,
		Topic:          message.Topic,
		Partition:      message.Partition,
		Offset:         message.Offset,
		Timestamp:      message.Timestamp,
		BlockTimestamp: message.BlockTimestamp,
		Headers:        headers,
	}

	return ctx, span, msg
}

func (g *groupHandler) handleMessageResult(
	ctx context.Context,
	span trace.Span,
	session sarama.ConsumerGroupSession,
	message *sarama.ConsumerMessage,
	err error,
) error {
	switch {
	case err == nil:
		metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "success").Inc()
		session.MarkMessage(message, "")
		return nil

	case errors.Is(err, ErrMessageHandledByDLQ):
		metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "dlq").Inc()
		session.MarkMessage(message, "")
		return nil

	case errors.Is(err, ErrSkipMessage):
		tracing.RecordError(span, err)
		metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "skipped").Inc()

		g.logger.Warn(ctx, "Kafka message skipped",
			zap.String("topic", message.Topic),
			zap.Int32("partition", message.Partition),
			zap.Int64("offset", message.Offset),
			zap.Error(err),
		)

		session.MarkMessage(message, "")
		return nil

	case errors.Is(err, ErrRestartConsumerSession):
		tracing.RecordError(span, err)
		metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "session_restart").Inc()

		g.logger.Warn(ctx, "Kafka consumer session restart requested",
			zap.String("topic", message.Topic),
			zap.Int32("partition", message.Partition),
			zap.Int64("offset", message.Offset),
			zap.Error(err),
		)

		return err

	case IsNonRetryableError(err):
		tracing.RecordError(span, err)
		metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "skipped").Inc()

		g.logger.Warn(ctx, "Kafka message is non-retryable and will be skipped",
			zap.String("topic", message.Topic),
			zap.Int32("partition", message.Partition),
			zap.Int64("offset", message.Offset),
			zap.Error(err),
		)

		session.MarkMessage(message, "")
		return nil

	default:
		tracing.RecordError(span, err)
		metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "retry").Inc()

		g.logger.Error(ctx, "Kafka handler error, message will be retried",
			zap.String("topic", message.Topic),
			zap.Int32("partition", message.Partition),
			zap.Int64("offset", message.Offset),
			zap.Error(err),
		)

		return err
	}
}

func extractHeaders(headers []*sarama.RecordHeader) map[string][]byte {
	result := make(map[string][]byte, len(headers))
	for _, header := range headers {
		if header != nil && header.Key != nil {
			result[string(header.Key)] = header.Value
		}
	}

	return result
}
