package logger

import (
	"context"
	"log"
	"path"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func LoggerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		method := path.Base(info.FullMethod)

		log.Printf("Started gRPC method %s\n", method)
		startTime := time.Now()

		response, err := handler(ctx, request)

		duration := time.Since(startTime)

		if err != nil {
			responseStatus, _ := status.FromError(err)
			log.Printf("Finished gRPC method %s with code %s: %v (took: %v)\n",
				method, responseStatus.Code(), err, duration)
		} else {
			log.Printf("Finished gRPC method %s successfully (took: %v)\n", method, duration)
		}

		return response, err
	}
}
