package errors

import (
	"context"
	"errors"

	"github.com/sony/gobreaker/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
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
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}

	stat, ok := status.FromError(err)
	if ok && stat.Code() != codes.Unknown {
		return err
	}

	switch {
	case errors.Is(err, service.ErrMarketsNotFound),
		errors.Is(err, service.ErrMarketNotFound),
		errors.Is(err, service.ErrOrderNotFound):
		logger.Warn(ctx, "resource not found", zap.Error(err))
		return status.Error(codes.NotFound, "resource not found")

	case errors.Is(err, service.ErrMarketUnavailable):
		logger.Warn(ctx, "market temporarily unavailable", zap.Error(err))
		return status.Error(codes.Unavailable, "market temporarily unavailable")

	case errors.Is(err, service.ErrMarketsUnavailable):
		logger.Warn(ctx, "markets are temporarily unavailable", zap.Error(err))
		return status.Error(codes.Unavailable, err.Error())

	case errors.Is(err, service.ErrOrderAlreadyExists):
		logger.Warn(ctx, "order already exists", zap.Error(err))
		return status.Error(codes.AlreadyExists, "order already exists")

	case errors.Is(err, service.ErrRateLimitExceeded):
		logger.Warn(ctx, "rate limit exceeded", zap.Error(err))
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, gobreaker.ErrOpenState),
		errors.Is(err, gobreaker.ErrTooManyRequests):
		return status.Error(codes.Unavailable, "service temporarily unavailable")

	case errors.Is(err, service.ErrMarketDisabled):
		logger.Warn(ctx, "market is disabled", zap.Error(err))
		return status.Error(codes.FailedPrecondition, "market is disabled")

	case errors.Is(err, service.ErrOrderProcessing):
		logger.Warn(ctx, "order is processing", zap.Error(err))
		return status.Error(codes.FailedPrecondition, "order is already being processed, wait please")

	case errors.Is(err, service.ErrUserRoleNotSpecified),
		errors.Is(err, service.ErrMissingMetadata),
		errors.Is(err, service.ErrMissingAuthToken),
		errors.Is(err, service.ErrInvalidToken),
		errors.Is(err, service.ErrTokenExpired),
		errors.Is(err, service.ErrInvalidTokenType),
		errors.Is(err, service.ErrMissingTokenSubject),
		errors.Is(err, service.ErrMissingTokenSessionID),
		errors.Is(err, service.ErrMissingUserRoles),
		errors.Is(err, service.ErrInvalidUserRoles),
		errors.Is(err, service.ErrInvalidUserIDInToken),
		errors.Is(err, service.ErrInvalidSubject),
		errors.Is(err, service.ErrInvalidJTI),
		errors.Is(err, service.ErrTokenRevoked):
		logger.Warn(ctx, "authentication failed", zap.Error(err))
		return status.Error(codes.Unauthenticated, "authentication failed")

	case errors.Is(err, service.ErrSessionValidationFailed),
		errors.Is(err, service.ErrSaveTokenFailed),
		errors.Is(err, service.ErrSignAccessTokenFailed),
		errors.Is(err, service.ErrSignRefreshTokenFailed),
		errors.Is(err, service.ErrBuildTokenClaimsFailed),
		errors.Is(err, service.ErrInternalAuthContext):
		logger.Error(ctx, "authentication internal failure", zap.Error(err))
		return status.Error(codes.Internal, "internal error")

	default:
		logger.Error(ctx, "unhandled error", zap.Error(err))
		return status.Error(codes.Internal, "internal error")
	}
}

func CodeFromError(err error) codes.Code {
	if err == nil {
		return codes.OK
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Code()
	}

	return status.Code(err)
}
