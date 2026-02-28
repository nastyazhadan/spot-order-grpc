package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/domain/models"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/dto"
	repositoryErrors "github.com/nastyazhadan/spot-order-grpc/shared/errors/repository"
)

const PostgresErrorCode = "23505"

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
		if isDuplicateKey(err) {
			return fmt.Errorf("%s: %w", op, repositoryErrors.ErrOrderAlreadyExists)
		}

		return fmt.Errorf("%s: exec: %w", op, err)
	}

	return nil
}

func (o *OrderStore) GetOrder(ctx context.Context, id uuid.UUID) (models.Order, error) {
	const op = "infrastructure.OrderStore.GetOrderStatus"

	rows, err := o.pool.Query(ctx,
		`SELECT id, user_id, market_id, type, price, quantity, status, created_at
		 FROM orders
		 WHERE id = $1
		 LIMIT 1`,
		id,
	)

	if err != nil {
		return models.Order{}, fmt.Errorf("%s: query: %w", op, err)
	}

	orderDTO, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[dto.Order])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Order{}, fmt.Errorf("%s: %w", op, repositoryErrors.ErrOrderNotFound)
		}

		return models.Order{}, fmt.Errorf("%s: collect: %w", op, err)
	}

	return orderDTO.ToDomain(), nil
}

func isDuplicateKey(err error) bool {
	var postgresErr *pgconn.PgError

	if errors.As(err, &postgresErr) {
		return postgresErr.Code == PostgresErrorCode
	}

	return false
}
