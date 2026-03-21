package postgres

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

	mapper "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/outbound"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	sharedErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

const (
	serviceName         = "orderService"
	uniqueViolationCode = "23505"
)

type OrderStore struct {
	pool *pgxpool.Pool
}

func NewOrderStore(pool *pgxpool.Pool) *OrderStore {
	return &OrderStore{
		pool: pool,
	}
}

func (o *OrderStore) SaveOrder(ctx context.Context, order models.Order) error {
	const op = "infrastructure.OrderStore.SaveOrder"

	ctx, span := tracing.StartSpan(ctx, "postgres.save_order",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("order_id", order.ID.String()),
			attribute.String("user_id", order.UserID.String()),
			attribute.String("market_id", order.MarketID.String()),
			attribute.String("price", order.Price.String()),
			attribute.Int64("quantity", order.Quantity),
			attribute.String("order_type", order.Type.String()),
			attribute.String("order_status", order.Status.String()),
		),
	)
	defer span.End()

	orderDTO := mapper.FromDomain(order)

	start := time.Now()
	_, err := o.pool.Exec(ctx,
		`INSERT INTO orders (id, user_id, market_id, type, price, quantity, status, created_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		orderDTO.ID,
		orderDTO.UserID,
		orderDTO.MarketID,
		orderDTO.Type,
		orderDTO.Price,
		orderDTO.Quantity,
		orderDTO.Status,
		orderDTO.CreatedAt,
	)
	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(serviceName, "save_order"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		if isPrimaryKeyViolation(err) {
			return fmt.Errorf("%s: %w", op, sharedErrors.ErrAlreadyExists{ID: orderDTO.ID})
		}

		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (o *OrderStore) GetOrder(ctx context.Context, id uuid.UUID) (models.Order, error) {
	const op = "infrastructure.OrderStore.GetOrder"

	ctx, span := tracing.StartSpan(ctx, "postgres.get_order",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("order_id", id.String())),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.ObserveWithTrace(ctx,
			metrics.DBQueryDuration.WithLabelValues(serviceName, "get_order"),
			time.Since(start).Seconds(),
		)
	}()

	rows, err := o.pool.Query(ctx,
		`SELECT id, user_id, market_id, type, price, quantity, status, created_at
		 FROM orders
		 WHERE id = $1`,
		id,
	)
	if err != nil {
		tracing.RecordError(span, err)
		return models.Order{}, fmt.Errorf("%s: %w", op, err)
	}

	orderDTO, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[mapper.Order])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Order{}, fmt.Errorf("%s: %w", op, sharedErrors.ErrNotFound{ID: id})
		}
		tracing.RecordError(span, err)

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

func isPrimaryKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == "orders_pkey"
	}

	return false
}
