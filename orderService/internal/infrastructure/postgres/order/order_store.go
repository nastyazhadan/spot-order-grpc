package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	mapper "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/outbound/postgres"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

const (
	databaseName        = "postgresql"
	uniqueViolationCode = "23505"
	constraintName      = "orders_pkey"
)

type OrderStore struct {
	pool   *pgxpool.Pool
	config config.OrderConfig
}

func New(pool *pgxpool.Pool, cfg config.OrderConfig) *OrderStore {
	return &OrderStore{
		pool:   pool,
		config: cfg,
	}
}

func (o *OrderStore) SaveOrder(ctx context.Context, transaction pgx.Tx, order models.Order) error {
	const op = "infrastructure.OrderStore.SaveOrder"

	ctx, span := tracing.StartSpan(ctx, "postgres.save_order_transaction",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", databaseName),
			attribute.String("order_id", order.ID.String()),
			attribute.String("user_id", order.UserID.String()),
			attribute.String("market_id", order.MarketID.String()),
		),
	)
	defer span.End()

	orderDTO := mapper.FromDomain(order)

	start := time.Now()
	_, err := transaction.Exec(ctx,
		`INSERT INTO orders (id, user_id, market_id, type, price, quantity, status, created_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		orderDTO.ID, orderDTO.UserID, orderDTO.MarketID,
		orderDTO.Type, orderDTO.Price, orderDTO.Quantity,
		orderDTO.Status, orderDTO.CreatedAt,
	)
	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(o.config.Service.Name, "save_order_transaction"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		if isPrimaryKeyViolation(err) {
			return fmt.Errorf("%s: %w", op, repositoryErrors.ErrOrderAlreadyExists)
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (o *OrderStore) GetOrder(ctx context.Context, id, userID uuid.UUID) (models.Order, error) {
	const op = "infrastructure.OrderStore.GetOrder"

	ctx, span := tracing.StartSpan(ctx, "postgres.get_order",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", databaseName),
			attribute.String("order_id", id.String()),
			attribute.String("user_id", userID.String()),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(ctx,
			metrics.DBQueryDuration.WithLabelValues(o.config.Service.Name, "get_order"),
			time.Since(start).Seconds(),
		)
	}()

	rows, err := o.pool.Query(ctx,
		`SELECT id, user_id, market_id, type, price, quantity, status, created_at
		 FROM orders
		 WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		tracing.RecordError(span, err)
		return models.Order{}, fmt.Errorf("%s: %w", op, err)
	}

	orderDTO, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[mapper.Order])
	if err != nil {
		tracing.RecordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Order{}, fmt.Errorf("%s: %w", op, repositoryErrors.ErrOrderNotFound)
		}

		return models.Order{}, fmt.Errorf("%s: %w", op, err)
	}

	order, err := orderDTO.ToDomain()
	if err != nil {
		tracing.RecordError(span, err)
		return models.Order{}, fmt.Errorf("%s: %w", op, err)
	}

	span.SetAttributes(
		attribute.String("order_status", order.Status.String()),
		attribute.String("order_type", order.Type.String()),
	)

	return order, nil
}

func (o *OrderStore) UpdateOrderStatus(
	ctx context.Context,
	transaction pgx.Tx,
	orderID uuid.UUID,
	status shared.OrderStatus,
) error {
	const op = "OrderStore.UpdateOrderStatus"

	ctx, span := tracing.StartSpan(ctx, "order.update_status",
		trace.WithAttributes(
			attribute.String("order_id", orderID.String()),
			attribute.Int("status", int(status)),
		),
	)
	defer span.End()

	start := time.Now()
	result, err := transaction.Exec(ctx, `
		UPDATE orders
		SET status = $2
		WHERE id = $1
	`, orderID, int16(status))

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(o.config.Service.Name, "order.update_status"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	if result.RowsAffected() == 0 {
		err = repositoryErrors.ErrOrderNotFound
		tracing.RecordError(span, err)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func isPrimaryKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == constraintName
	}

	return false
}
