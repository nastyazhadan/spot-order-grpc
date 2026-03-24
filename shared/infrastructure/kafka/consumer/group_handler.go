package consumer

import (
	"errors"
	"time"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka"
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

func NewGroupHandler(handler MessageHandler, name string, logger *zapLogger.Logger) *groupHandler {
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

func (g *groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message, ok := <-claim.Messages():
			if !ok {
				g.logger.Info(session.Context(), "Kafka message channel closed")
				return nil
			}

			headers := extractHeaders(message.Headers)

			carrier := kafka.HeadersCarrier(headers)
			ctx := otel.GetTextMapPropagator().Extract(session.Context(), carrier)

			ctx, span := otel.Tracer(instrumentationName).Start(
				ctx,
				"kafka.consume",
				trace.WithSpanKind(trace.SpanKindConsumer),
				trace.WithAttributes(
					attribute.String("messaging.system", kafka.SystemName),
					attribute.String("messaging.source", message.Topic),
					attribute.String("messaging.source_kind", kafka.DestinationKind),
					attribute.Int("messaging.kafka.partition", int(message.Partition)),
					attribute.Int64("messaging.kafka.offset", message.Offset),
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

			start := time.Now()
			err := g.handler(ctx, msg)
			elapsed := time.Since(start).Seconds()

			metrics.ObserveWithTrace(ctx,
				metrics.KafkaConsumeDuration.WithLabelValues(g.serviceName, message.Topic),
				elapsed,
			)

			if err != nil {
				if errors.Is(err, ErrMessageHandledByDLQ) {
					session.MarkMessage(message, "")
					span.End()
					continue
				}

				tracing.RecordError(span, err)
				metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "error").Inc()

				g.logger.Error(ctx, "Kafka handler error",
					zap.String("topic", message.Topic),
					zap.Int32("partition", message.Partition),
					zap.Int64("offset", message.Offset),
					zap.Error(err),
				)
				span.End()
				continue
			}

			metrics.KafkaMessagesConsumedTotal.WithLabelValues(g.serviceName, message.Topic, "success").Inc()

			session.MarkMessage(message, "")
			span.End()

		case <-session.Context().Done():
			g.logger.Info(session.Context(), "Kafka session context done")
			return nil
		}
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
