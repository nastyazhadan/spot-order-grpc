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
	"go.opentelemetry.io/otel/trace"

	mapper "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/outbound/postgres"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models/shared"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/otel/attributes"
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
			attributes.DBSystemValue(databaseName),
			attributes.OrderIDValue(order.ID.String()),
			attributes.UserIDValue(order.UserID.String()),
			attributes.MarketIDValue(order.MarketID.String()),
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
			attributes.DBSystemValue(databaseName),
			attributes.OrderIDValue(id.String()),
			attributes.UserIDValue(userID.String()),
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
		attributes.OrderStatusValue(order.Status.String()),
		attributes.OrderTypeValue(order.Type.String()),
	)

	return order, nil
}

func (o *OrderStore) CancelActiveOrdersByMarket(
	ctx context.Context,
	transaction pgx.Tx,
	marketID uuid.UUID,
) ([]uuid.UUID, error) {
	const op = "OrderStore.CancelActiveOrdersByMarket"

	ctx, span := tracing.StartSpan(ctx, "order.cancel_active_by_market",
		trace.WithAttributes(
			attributes.MarketIDValue(marketID.String()),
		),
	)
	defer span.End()

	start := time.Now()
	rows, err := transaction.Query(ctx, `
		UPDATE orders
		SET status = $2
		WHERE market_id = $1 AND status IN ($3, $4)
		RETURNING id
	`,
		marketID,
		int16(shared.OrderStatusCancelled),
		int16(shared.OrderStatusCreated),
		int16(shared.OrderStatusPending),
	)

	metrics.ObserveWithTrace(ctx,
		metrics.DBQueryDuration.WithLabelValues(o.config.Service.Name, "order.cancel_active_by_market"),
		time.Since(start).Seconds(),
	)

	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	cancelledIDs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		if scanErr := row.Scan(&id); scanErr != nil {
			return uuid.Nil, scanErr
		}
		return id, nil
	})
	if err != nil {
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("%s: collect cancelled order ids: %w", op, err)
	}

	span.SetAttributes(attributes.OrdersCancelledCountValue(len(cancelledIDs)))

	return cancelledIDs, nil
}

func isPrimaryKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == constraintName
	}

	return false
}
