package producer

import (
	"context"
	"sync"
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

type publishResult struct {
	partition int32
	offset    int64
	err       error
}

type Client struct {
	asyncProducer sarama.AsyncProducer
	serviceName   string
	logger        *zapLogger.Logger

	wg        sync.WaitGroup
	closeOnce sync.Once
	closing   chan struct{}
	closed    chan struct{}
}

func NewClient(
	asyncProducer sarama.AsyncProducer,
	serviceName string,
	logger *zapLogger.Logger,
) *Client {
	client := &Client{
		asyncProducer: asyncProducer,
		serviceName:   serviceName,
		logger:        logger,
		closing:       make(chan struct{}),
		closed:        make(chan struct{}),
	}

	client.wg.Add(1)
	go client.runAckLoop()

	return client
}

func (c *Client) runAckLoop() {
	defer c.wg.Done()
	defer close(c.closed)

	successes := c.asyncProducer.Successes()
	errorsChannel := c.asyncProducer.Errors()

	for successes != nil || errorsChannel != nil {
		select {
		case message, ok := <-successes:
			if !ok {
				successes = nil
				continue
			}
			c.handleSuccess(message)

		case producerErr, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil
				continue
			}
			c.handleError(producerErr)
		}
	}
}

func (c *Client) handleSuccess(
	message *sarama.ProducerMessage,
) {
	request, ok := message.Metadata.(*publishRequest)
	if !ok || request == nil {
		return
	}

	elapsed := time.Since(request.started).Seconds()

	metrics.ObserveWithTrace(
		request.ctx,
		metrics.KafkaPublishDuration.WithLabelValues(c.serviceName, request.topic),
		elapsed,
	)
	metrics.KafkaMessagesPublishedTotal.WithLabelValues(c.serviceName, request.topic).Inc()

	c.logger.Info(request.ctx, "Kafka message published",
		zap.String("topic", request.topic),
		zap.Int32("partition", message.Partition),
		zap.Int64("offset", message.Offset),
		zap.Bool("has_key", request.hasKey),
		zap.Int("key_len", request.keyLength),
	)

	request.finish(publishResult{
		partition: message.Partition,
		offset:    message.Offset,
		err:       nil,
	})
}

func (c *Client) handleError(
	producerError *sarama.ProducerError,
) {
	if producerError == nil || producerError.Msg == nil {
		return
	}

	request, ok := producerError.Msg.Metadata.(*publishRequest)
	if !ok || request == nil {
		return
	}

	elapsed := time.Since(request.started).Seconds()

	tracing.RecordError(request.span, producerError.Err)

	metrics.ObserveWithTrace(
		request.ctx,
		metrics.KafkaPublishDuration.WithLabelValues(c.serviceName, request.topic),
		elapsed,
	)
	metrics.KafkaPublishErrorsTotal.WithLabelValues(c.serviceName, request.topic).Inc()

	c.logger.Error(request.ctx, "Failed to publish Kafka message",
		zap.String("topic", request.topic),
		zap.Error(producerError.Err),
	)

	request.finish(publishResult{
		err: producerError.Err,
	})
}

func (c *Client) publish(
	ctx context.Context,
	defaultTopic string,
	message kafka.Message,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	topic := defaultTopic
	if message.Topic != "" {
		topic = message.Topic
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

	carrier := kafka.HeadersCarrier{}
	for key, value := range message.Headers {
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

	request := &publishRequest{
		ctx:       ctx,
		span:      span,
		topic:     topic,
		started:   time.Now(),
		hasKey:    len(message.Key) > 0,
		keyLength: len(message.Key),
		done:      make(chan struct{}),
	}

	producerMessage := &sarama.ProducerMessage{
		Topic:    topic,
		Key:      sarama.ByteEncoder(message.Key),
		Value:    sarama.ByteEncoder(message.Value),
		Headers:  headers,
		Metadata: request,
	}

	if err := c.enqueue(ctx, topic, request, producerMessage); err != nil {
		return err
	}

	return c.waitPublishResult(ctx, request)
}

func (c *Client) enqueue(
	ctx context.Context,
	topic string,
	request *publishRequest,
	message *sarama.ProducerMessage,
) error {
	select {
	case <-ctx.Done():
		tracing.RecordError(request.span, ctx.Err())
		request.finish(publishResult{err: ctx.Err()})
		return ctx.Err()

	case <-c.closing:
		c.logger.Warn(
			ctx,
			"Kafka producer is closing, message publish rejected",
			zap.String("topic", topic),
		)

		tracing.RecordError(request.span, ErrProducerUnavailable)
		request.finish(publishResult{err: ErrProducerUnavailable})
		return ErrProducerUnavailable

	case c.asyncProducer.Input() <- message:
		return nil
	}
}

func (c *Client) waitPublishResult(
	ctx context.Context,
	request *publishRequest,
) error {
	for {
		select {
		case <-request.done:
			return request.result.err

		case <-ctx.Done():
			c.logger.Warn(
				ctx,
				"Kafka publish wait canceled by context",
				zap.String("topic", request.topic),
				zap.Error(ctx.Err()),
			)
			tracing.RecordError(request.span, ctx.Err())
			return ctx.Err()

		case <-c.closed:
			if request.isDone() {
				return request.result.err
			}

			c.logger.Error(
				ctx,
				"Kafka producer stopped before publish result was received",
				zap.String("topic", request.topic),
			)

			tracing.RecordError(request.span, ErrProducerUnavailable)
			request.finish(publishResult{err: ErrProducerUnavailable})
			return ErrProducerUnavailable
		}
	}
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closing)
		c.asyncProducer.AsyncClose()
		c.wg.Wait()
	})

	return nil
}
