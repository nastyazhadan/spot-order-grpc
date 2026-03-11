package errors

import (
	"context"
	"errors"

	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ErrorMappingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		response, err := handler(ctx, request)
		if err != nil {
			return nil, mapError(ctx, err)
		}
		return response, nil
	}
}

func mapError(ctx context.Context, err error) error {
	if _, found := status.FromError(err); found {
		return err
	}

	switch {
	case errors.Is(err, serviceErrors.ErrMarketsNotFound),
		errors.Is(err, serviceErrors.ErrOrderNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, serviceErrors.ErrOrderAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, serviceErrors.ErrRateLimitExceeded):
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, serviceErrors.ErrCreatingOrderNotRequired),
		errors.Is(err, serviceErrors.ErrUserRoleNotSpecified):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, gobreaker.ErrOpenState),
		errors.Is(err, gobreaker.ErrTooManyRequests):
		return status.Error(codes.Unavailable, "service temporarily unavailable")

	default:
		zapLogger.Error(ctx, "unhandled error", zap.Error(err))
		return status.Error(codes.Internal, "internal error")
	}
}
