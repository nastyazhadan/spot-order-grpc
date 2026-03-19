package validate

import (
	"context"
	"fmt"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	validator, err := protovalidate.New()
	if err != nil {
		panic(fmt.Errorf("protovalidate.New: %w", err))
	}

	return func(
		context context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if message, ok := request.(proto.Message); ok {
			if validateErr := validator.Validate(message); validateErr != nil {
				return nil, status.Error(codes.InvalidArgument, validateErr.Error())
			}
		}
		return handler(context, request)
	}
}
