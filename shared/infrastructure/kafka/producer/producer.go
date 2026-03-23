package producer

import (
	"context"
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
	ctx, span := otel.Tracer(instrumentationName).Start(
		ctx,
		"kafka.produce",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", kafka.SystemName),
			attribute.String("messaging.destination", p.topic),
			attribute.String("messaging.destination_kind", kafka.DestinationKind),
		),
	)
	defer span.End()

	carrier := kafka.HeadersCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	headers := make([]sarama.RecordHeader, 0, len(carrier))
	for k, v := range carrier {
		headers = append(headers, sarama.RecordHeader{Key: []byte(k), Value: v})
	}

	start := time.Now()
	partition, offset, err := p.syncProducer.SendMessage(&sarama.ProducerMessage{
		Topic:   p.topic,
		Key:     sarama.ByteEncoder(key),
		Value:   sarama.ByteEncoder(value),
		Headers: headers,
	})
	elapsed := time.Since(start).Seconds()

	metrics.ObserveWithTrace(ctx,
		metrics.KafkaPublishDuration.WithLabelValues(p.serviceName, p.topic),
		elapsed,
	)

	if err != nil {
		tracing.RecordError(span, err)
		metrics.KafkaPublishErrorsTotal.WithLabelValues(p.serviceName, p.topic).Inc()
		p.logger.Error(ctx, "Failed to publish Kafka message",
			zap.String("topic", p.topic),
			zap.Error(err))
		return err
	}

	metrics.KafkaMessagesPublishedTotal.WithLabelValues(p.serviceName, p.topic).Inc()

	p.logger.Info(ctx, "Kafka message published",
		zap.String("topic", p.topic),
		zap.Int32("partition", partition),
		zap.Int64("offset", offset),
		zap.String("key", string(key)),
	)

	return nil
}
