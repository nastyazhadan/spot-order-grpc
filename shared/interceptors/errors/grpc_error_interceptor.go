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
		_ *grpc.UnaryServerInfo,
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
	// Штатные ошибки - warn, ошибки системы - error
	case errors.Is(err, serviceErrors.ErrMarketsNotFound),
		errors.Is(err, serviceErrors.ErrMrktNotFound),
		errors.Is(err, serviceErrors.ErrOrderNotFound):
		logger.Warn(ctx, "resource not found", zap.Error(err))
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, serviceErrors.ErrOrderAlreadyExists):
		logger.Warn(ctx, "order already exists", zap.Error(err))
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, serviceErrors.ErrRateLimitExceeded):
		logger.Warn(ctx, "rate limit exceeded", zap.Error(err))
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, serviceErrors.ErrUserRoleNotSpecified):
		logger.Warn(ctx, "user role not specified", zap.Error(err))
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, serviceErrors.ErrInvalidSubject),
		errors.Is(err, serviceErrors.ErrInvalidJTI),
		errors.Is(err, serviceErrors.ErrTokenRevoked):
		logger.Warn(ctx, "refresh token error", zap.Error(err))
		return status.Error(codes.Unauthenticated, err.Error())

	case errors.Is(err, gobreaker.ErrOpenState),
		errors.Is(err, gobreaker.ErrTooManyRequests):
		return status.Error(codes.Unavailable, "service temporarily unavailable")

	case errors.Is(err, serviceErrors.ErrRevokeTokenFailed),
		errors.Is(err, serviceErrors.ErrSaveTokenFailed):
		logger.Error(ctx, "token store failure", zap.Error(err))
		return status.Error(codes.Internal, err.Error())

	default:
		logger.Error(ctx, "unhandled error", zap.Error(err))
		return status.Error(codes.Internal, "internal error")
	}
}
