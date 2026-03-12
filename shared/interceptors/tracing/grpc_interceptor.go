package tracing

import (
	"context"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	TraceIDHeader = "x-trace-id"
)

func UnaryServerInterceptor(serviceName string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context,
		request interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		tracer := otel.GetTracerProvider().Tracer(serviceName)
		propagator := otel.GetTextMapPropagator()

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}

		ctx = propagator.Extract(ctx, metadataCarrier(md))

		ctx, span := tracer.Start(
			ctx,
			info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		ctx = zapLogger.ContextWithTraceID(ctx, span.SpanContext().TraceID().String())

		AddTraceIDToResponse(ctx)

		response, err := handler(ctx, request)
		if err != nil {
			span.RecordError(err)
		}

		return response, err
	}
}

func UnaryClientInterceptor(serviceName string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context,
		method string,
		request, reply interface{},
		connection *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		options ...grpc.CallOption,
	) error {
		tracer := otel.GetTracerProvider().Tracer(serviceName)
		propagator := otel.GetTextMapPropagator()

		spanName := formatSpanName(ctx, method)

		ctx, span := tracer.Start(
			ctx,
			spanName,
			trace.WithSpanKind(trace.SpanKindClient),
		)
		defer span.End()

		carrier := metadataCarrier(extractOutgoingMetadata(ctx))

		propagator.Inject(ctx, carrier)

		ctx = metadata.NewOutgoingContext(ctx, metadata.MD(carrier))

		err := invoker(ctx, method, request, reply, connection, options...)
		if err != nil {
			span.RecordError(err)
		}

		return err
	}
}

func formatSpanName(ctx context.Context, method string) string {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return "client." + method
	}

	return method
}

func extractOutgoingMetadata(ctx context.Context) metadata.MD {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return metadata.New(nil)
	}

	return md.Copy()
}

func GetTraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}

	return span.SpanContext().TraceID().String()
}

func AddTraceIDToResponse(ctx context.Context) {
	traceID := GetTraceIDFromContext(ctx)
	if traceID == "" {
		return
	}

	_ = grpc.SetHeader(ctx, metadata.Pairs(TraceIDHeader, traceID))
}
