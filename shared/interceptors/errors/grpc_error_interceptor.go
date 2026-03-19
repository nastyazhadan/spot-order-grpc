package errors

import (
	"context"
	"errors"

	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryServerInterceptor(logger *zapLogger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		serverInfo *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		response, err := handler(ctx, request)
		if err != nil {
			return nil, mapError(ctx, err, logger)
		}
		return response, nil
	}
}

func mapError(ctx context.Context, err error, logger *zapLogger.Logger) error {
	if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
		return err
	}

	switch {
	case errors.Is(err, serviceErrors.ErrMarketsNotFound),
		errors.Is(err, serviceErrors.ErrMrktNotFound),
		errors.Is(err, serviceErrors.ErrOrderNotFound):
		logger.Error(ctx, "resource not found", zap.Error(err))
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, serviceErrors.ErrOrderAlreadyExists):
		logger.Error(ctx, "order already exists", zap.Error(err))
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, serviceErrors.ErrRateLimitExceeded):
		logger.Error(ctx, "rate limit exceeded", zap.Error(err))
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, serviceErrors.ErrUserRoleNotSpecified):
		logger.Error(ctx, "user role not specified", zap.Error(err))
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, gobreaker.ErrOpenState),
		errors.Is(err, gobreaker.ErrTooManyRequests):
		return status.Error(codes.Unavailable, "service temporarily unavailable")

	default:
		logger.Error(ctx, "unhandled error", zap.Error(err))
		return status.Error(codes.Internal, "internal error")
	}
}
