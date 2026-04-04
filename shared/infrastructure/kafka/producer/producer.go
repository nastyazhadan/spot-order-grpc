package producer

import (
	"context"
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

const instrumentationName = "spot-order-grpc/kafka/producer"

type producer struct {
	syncProducer sarama.SyncProducer
	topic        string
	serviceName  string
	logger       *zapLogger.Logger
}

func New(
	syncProducer sarama.SyncProducer,
	topic, serviceName string,
	logger *zapLogger.Logger,
) *producer {
	return &producer{
		syncProducer: syncProducer,
		topic:        topic,
		serviceName:  serviceName,
		logger:       logger,
	}
}

func (p *producer) Send(ctx context.Context, key, value []byte) error {
	return p.sendMessage(ctx, kafka.Message{
		Key:   key,
		Value: value,
	})
}

func (p *producer) SendMessage(ctx context.Context, msg kafka.Message) error {
	return p.sendMessage(ctx, msg)
}

func (p *producer) sendMessage(ctx context.Context, msg kafka.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	topic := p.topic
	if msg.Topic != "" {
		topic = msg.Topic
	}

	ctx, span := otel.Tracer(instrumentationName).Start(
		ctx,
		"kafka.produce",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attributes.MessagingSystemValue(kafka.SystemName),
			attributes.MessagingDestinationValue(topic),
			attributes.MessagingDestinationKindValue(kafka.DestinationKind),
		),
	)
	defer span.End()

	carrier := kafka.HeadersCarrier{}

	for key, value := range msg.Headers {
		copied := make([]byte, len(value))
		copy(copied, value)
		carrier[key] = copied
	}

	otel.GetTextMapPropagator().Inject(ctx, carrier)

	headers := make([]sarama.RecordHeader, 0, len(carrier))
	for key, value := range carrier {
		headers = append(headers, sarama.RecordHeader{
			Key:   []byte(key),
			Value: value,
		})
	}

	start := time.Now()
	partition, offset, err := p.syncProducer.SendMessage(&sarama.ProducerMessage{
		Topic:   topic,
		Key:     sarama.ByteEncoder(msg.Key),
		Value:   sarama.ByteEncoder(msg.Value),
		Headers: headers,
	})
	elapsed := time.Since(start).Seconds()

	metrics.ObserveWithTrace(ctx,
		metrics.KafkaPublishDuration.WithLabelValues(p.serviceName, topic),
		elapsed,
	)

	if err != nil {
		tracing.RecordError(span, err)
		metrics.KafkaPublishErrorsTotal.WithLabelValues(p.serviceName, topic).Inc()
		p.logger.Error(ctx, "Failed to publish Kafka message",
			zap.String("topic", topic),
			zap.Error(err))

		return err
	}

	metrics.KafkaMessagesPublishedTotal.WithLabelValues(p.serviceName, topic).Inc()

	p.logger.Info(ctx, "Kafka message published",
		zap.String("topic", topic),
		zap.Int32("partition", partition),
		zap.Int64("offset", offset),
		zap.Bool("has_key", len(msg.Key) > 0),
		zap.Int("key_len", len(msg.Key)),
	)

	return nil
}
