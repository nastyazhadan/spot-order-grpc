package tracing

import (
	"context"

	"github.com/nastyazhadan/spot-order-grpc/shared/requestctx"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		tracer := otel.Tracer(instrumentationName)
		propagator := otel.GetTextMapPropagator()

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}

		ctx = propagator.Extract(ctx, metadataCarrier(md))

		ctx, span := tracer.Start(
			ctx,
			serverInfo.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		response, err := handler(ctx, request)
		if err != nil {
			span.RecordError(err)
		}

		addTraceIDToResponse(ctx)

		return response, err
	}
}

func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context,
		method string,
		request, reply any,
		connection *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		options ...grpc.CallOption,
	) error {
		tracer := otel.Tracer(instrumentationName)
		propagator := otel.GetTextMapPropagator()

		ctx, span := tracer.Start(
			ctx,
			method,
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

func extractOutgoingMetadata(ctx context.Context) metadata.MD {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return metadata.New(nil)
	}

	return md.Copy()
}

func addTraceIDToResponse(ctx context.Context) {
	traceID, ok := requestctx.TraceIDFromContext(ctx)
	if !ok {
		return
	}

	_ = grpc.SetHeader(ctx, metadata.Pairs(requestctx.TraceIDHeader, traceID))
}
