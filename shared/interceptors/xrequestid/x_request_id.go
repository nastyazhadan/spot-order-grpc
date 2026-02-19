package xrequestid

import (
	"context"

	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const headerKey = "x-request-id"

func Server(
	ctx context.Context,
	request interface{},
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	requestID := ""

	if meta, found := metadata.FromIncomingContext(ctx); found {
		if values := meta.Get(headerKey); len(values) > 0 {
			requestID = values[0]
		}
	}

	if requestID == "" {
		requestID = uuid.New().String()
	}

	ctx = zapLogger.ContextWithTraceID(ctx, requestID)

	return handler(ctx, request)
}

func Client(
	ctx context.Context,
	method string,
	request interface{},
	reply interface{},
	clientConn *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	options ...grpc.CallOption,
) error {
	requestID := zapLogger.TraceIDFromContext(ctx)

	if requestID == "" {
		requestID = uuid.New().String()
	}

	ctx = metadata.AppendToOutgoingContext(ctx, headerKey, requestID)

	return invoker(ctx, method, request, reply, clientConn, options...)
}
