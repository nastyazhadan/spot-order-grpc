package auth

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func UnaryClientAuthInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		request, reply any,
		connection *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		incomingMetadata, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return invoker(ctx, method, request, reply, connection, opts...)
		}

		authValues := incomingMetadata.Get(authorizationHeader)
		if len(authValues) == 0 {
			return invoker(ctx, method, request, reply, connection, opts...)
		}

		authHeader := authValues[0]

		outgoingMetadata, _ := metadata.FromOutgoingContext(ctx)
		outgoingMetadata = outgoingMetadata.Copy()
		outgoingMetadata.Set(authorizationHeader, authHeader)

		ctx = metadata.NewOutgoingContext(ctx, outgoingMetadata)
		return invoker(ctx, method, request, reply, connection, opts...)
	}
}
