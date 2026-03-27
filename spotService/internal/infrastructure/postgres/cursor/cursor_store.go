package cursor

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dto "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/dto/outbound/postgres"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/domain/models"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Get(ctx context.Context, pollerName string) (models.PollerCursor, error) {
	const op = "CursorStore.Get"

	var cursor dto.PollerCursor

	err := s.pool.QueryRow(ctx, `
		SELECT poller_name, last_seen_at, last_seen_id
		FROM market_poller_cursor
		WHERE poller_name = $1
	`, pollerName).Scan(&cursor.PollerName, &cursor.LastSeenAt, &cursor.LastSeenID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.PollerCursor{}, pgx.ErrNoRows
		}
		return models.PollerCursor{}, fmt.Errorf("%s: %w", op, err)
	}

	return dto.ToDomain(cursor), nil
}

func (s *Store) SaveCursorTransaction(ctx context.Context, transaction pgx.Tx, cursor models.PollerCursor) error {
	const op = "CursorStore.SaveCursorTransaction"

	dtoCursor := dto.FromDomain(cursor)

	_, err := transaction.Exec(ctx, `
		INSERT INTO market_poller_cursor (poller_name, last_seen_at, last_seen_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (poller_name)
		DO UPDATE SET
			last_seen_at = EXCLUDED.last_seen_at,
			last_seen_id = EXCLUDED.last_seen_id,
			updated_at = NOW()
	`, dtoCursor.PollerName, dtoCursor.LastSeenAt.UTC(), dtoCursor.LastSeenID)

	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
