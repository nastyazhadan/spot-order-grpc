package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	dto "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/dto/outbound"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors/repository"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
)

const ErrorCodePostgres = "23505"

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
		trace.WithAttributes(
			attributeUUID("order_id", order.ID),
			attributeUUID("user_id", order.UserID),
			attributeUUID("market_id", order.MarketID),
		),
	)
	defer span.End()

	orderDTO := dto.FromDomain(order)

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

	if err != nil {
		span.RecordError(err)
		if isDuplicateKey(err) {
			return fmt.Errorf("%s: %w", op, repositoryErrors.ErrAlreadyExists{ID: orderDTO.ID})
		}

		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (o *OrderStore) GetOrder(ctx context.Context, id uuid.UUID) (models.Order, error) {
	const op = "infrastructure.OrderStore.GetOrderStatus"

	ctx, span := tracing.StartSpan(ctx, "postgres.get_order",
		trace.WithAttributes(
			attributeUUID("order_id", id),
		),
	)
	defer span.End()

	rows, err := o.pool.Query(ctx,
		`SELECT id, user_id, market_id, type, price, quantity, status, created_at
		 FROM orders
		 WHERE id = $1
		 LIMIT 1`,
		id,
	)
	if err != nil {
		span.RecordError(err)
		return models.Order{}, fmt.Errorf("%s: %w", op, err)
	}

	orderDTO, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[dto.Order])
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Order{}, fmt.Errorf("%s: %w", op, repositoryErrors.ErrNotFound{ID: id})
		}

		return models.Order{}, fmt.Errorf("%s: %w", op, err)
	}

	return orderDTO.ToDomain(), nil
}

func isDuplicateKey(err error) bool {
	var postgresErr *pgconn.PgError

	if errors.As(err, &postgresErr) {
		return postgresErr.Code == ErrorCodePostgres
	}

	return false
}

func attributeUUID(key string, id uuid.UUID) attribute.KeyValue {
	return attribute.String(key, id.String())
}
