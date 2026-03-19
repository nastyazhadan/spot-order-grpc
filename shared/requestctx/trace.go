package requestctx

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

const (
	// TraceIDHeader используется для удобства (для response header).
	// Для межсервисной передачи используется propagator в grpc_interceptor.go
	TraceIDHeader = "x-trace-id"
	TraceIDField  = "trace_id"
)

func TraceIDFromContext(ctx context.Context) (string, bool) {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return "", false
	}

	return spanCtx.TraceID().String(), true
}
