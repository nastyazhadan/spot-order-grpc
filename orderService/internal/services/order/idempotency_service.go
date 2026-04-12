package order

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	orderModel "github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	serviceErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/service"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

type IdempotencyResult struct {
	IsCompleted  bool
	IsProcessing bool
	OrderID      uuid.UUID
	OrderStatus  string
}

type IdempotencyAdapter interface {
	Acquire(ctx context.Context, userID uuid.UUID, requestHash string) (IdempotencyResult, bool, error)
	Complete(ctx context.Context, userID uuid.UUID, requestHash string, orderID uuid.UUID, orderStatus string) error
	FailCleanup(ctx context.Context, userID uuid.UUID, requestHash string) error
}

type IdempotencyService struct {
	idempotencyAdapter IdempotencyAdapter
	logger             *zapLogger.Logger
}

func NewIdempotencyService(adapter IdempotencyAdapter, logger *zapLogger.Logger) *IdempotencyService {
	return &IdempotencyService{
		idempotencyAdapter: adapter,
		logger:             logger,
	}
}

func (s *IdempotencyService) buildRequestHash(
	marketID uuid.UUID,
	orderType orderModel.OrderType,
	price orderModel.Decimal,
	quantity int64,
) string {
	raw := fmt.Sprintf("%s|%s|%s|%d",
		marketID.String(),
		orderType.String(),
		price.String(),
		quantity,
	)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}

func (s *IdempotencyService) acquire(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
) (IdempotencyResult, bool, error) {
	return s.idempotencyAdapter.Acquire(ctx, userID, requestHash)
}

func (s *IdempotencyService) checkIdempotencyResult(
	ctx context.Context,
	idemResult IdempotencyResult,
) (uuid.UUID, orderModel.OrderStatus, error) {
	const op = "checkIdempotencyResult"

	if idemResult.IsCompleted {
		s.logger.Info(ctx, "idempotent response: returning cached order",
			zap.String("order_id", idemResult.OrderID.String()),
		)
		return idemResult.OrderID, orderModel.FromString(idemResult.OrderStatus), nil
	}

	if idemResult.IsProcessing {
		// Если дубликат прилетел пока первый ещё обрабатывается
		return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %w", op, serviceErrors.ErrOrderProcessing)
	}

	return uuid.Nil, orderModel.OrderStatusUnspecified, fmt.Errorf("%s: %s", op, "unknown idempotency state")
}

func (s *IdempotencyService) completeIdempotencyChecking(
	ctx context.Context,
	userID, orderID uuid.UUID,
	requestHash string,
	orderStatus orderModel.OrderStatus,
) {
	cleanupCtx := context.WithoutCancel(ctx)
	if err := s.idempotencyAdapter.Complete(
		cleanupCtx, userID, requestHash, orderID, orderStatus.String(),
	); err != nil {
		s.logger.Warn(ctx, "failed to mark idempotency key as completed",
			zap.String("order_id", orderID.String()),
			zap.Error(err),
		)
	}
}

func (s *IdempotencyService) failCleanup(
	ctx context.Context,
	userID uuid.UUID,
	requestHash string,
	acquired bool,
) {
	if !acquired {
		return
	}
	cleanupCtx := context.WithoutCancel(ctx)
	if err := s.idempotencyAdapter.FailCleanup(cleanupCtx, userID, requestHash); err != nil {
		s.logger.Warn(ctx, "idempotency fail cleanup error",
			zap.Error(err),
		)
	}
}
