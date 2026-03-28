package spot

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/cursor"
	outboxStore "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/outbox"
	spotStore "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/spot"
	spotCache "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis"
)

var InfraProviders = fx.Options(
	fx.Provide(
		provideLogger,

		providePostgresPool,
		provideRedisClient,
		provideCacheStore,
		provideMarketStore,
		provideMarketCursorStore,
		provideMarketCacheRepository,
		provideSpotOutboxStore,

		provideSaramaSyncProducer,
	),
)

func provideLogger(lifeCycle fx.Lifecycle, cfg config.SpotConfig) (*zapLogger.Logger, error) {
	logger := zapLogger.New(cfg.Log.Level, cfg.Log.Format == "json")

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return logger.Sync()
		},
	})

	return logger, nil
}

func providePostgresPool(cfg config.SpotConfig) (*pgxpool.Pool, error) {
	pool, err := db.NewPgxPool(cfg.Service.DBURI, db.PoolConfig{
		MaxConnections:  cfg.PostgresPool.MaxConnections,
		MinConnections:  cfg.PostgresPool.MinConnections,
		MaxConnLifetime: cfg.PostgresPool.MaxConnLifetime,
		MaxConnIdleTime: cfg.PostgresPool.MaxConnIdleTime,
	})
	if err != nil {
		return nil, fmt.Errorf("db.NewPgxPool: %w", err)
	}

	return pool, nil
}

func provideRedisClient(cfg config.SpotConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Address(),

		DialTimeout:  cfg.Redis.ConnectionTimeout,
		ReadTimeout:  cfg.Redis.ConnectionTimeout,
		WriteTimeout: cfg.Redis.ConnectionTimeout,

		PoolSize:       cfg.Redis.PoolSize,
		MinIdleConns:   cfg.Redis.MinIdle,
		MaxIdleConns:   cfg.Redis.MaxIdle,
		MaxActiveConns: cfg.Redis.MaxActiveConns,

		ConnMaxIdleTime: cfg.Redis.IdleTimeout,
		ConnMaxLifetime: cfg.Redis.ConnMaxLifetime,
	})

	return client, nil
}

func provideCacheStore(client *redis.Client) *cache.Store {
	return cache.New(client)
}

func provideMarketStore(pool *pgxpool.Pool, cfg config.SpotConfig) *spotStore.MarketStore {
	return spotStore.NewMarketStore(pool, cfg)
}

func provideMarketCursorStore(pool *pgxpool.Pool) *cursor.Store {
	return cursor.New(pool)
}

func provideMarketCacheRepository(
	store *cache.Store,
	cfg config.SpotConfig,
) *spotCache.MarketCacheRepository {
	return spotCache.NewMarketCacheRepository(store, cfg.Service.Name)
}

func provideSpotOutboxStore(
	pool *pgxpool.Pool,
	logger *zapLogger.Logger,
	cfg config.SpotConfig,
) *outboxStore.OutboxStore {
	return outboxStore.New(pool, logger, cfg)
}

func provideSaramaSyncProducer(cfg config.SpotConfig) (sarama.SyncProducer, error) {
	saramaCfg := sarama.NewConfig()
	saramaCfg.ClientID = cfg.Service.Name

	saramaCfg.Producer.Return.Successes = true
	saramaCfg.Producer.Return.Errors = true
	saramaCfg.Producer.RequiredAcks = sarama.WaitForAll

	saramaCfg.Producer.Timeout = cfg.Kafka.Producer.Timeout
	saramaCfg.Producer.Retry.Max = cfg.Kafka.Producer.MaxRetries
	saramaCfg.Producer.Retry.Backoff = cfg.Kafka.Producer.RetryBackoff

	saramaCfg.Net.DialTimeout = cfg.Kafka.Producer.Timeout
	saramaCfg.Net.ReadTimeout = cfg.Kafka.Producer.Timeout
	saramaCfg.Net.WriteTimeout = cfg.Kafka.Producer.Timeout

	saramaCfg.Metadata.Timeout = cfg.Kafka.Producer.Timeout
	saramaCfg.Metadata.Retry.Max = cfg.Kafka.Producer.MaxRetries
	saramaCfg.Metadata.Retry.Backoff = cfg.Kafka.Producer.RetryBackoff

	switch cfg.Kafka.Producer.Compression {
	case "gzip":
		saramaCfg.Producer.Compression = sarama.CompressionGZIP
	case "snappy":
		saramaCfg.Producer.Compression = sarama.CompressionSnappy
	case "lz4":
		saramaCfg.Producer.Compression = sarama.CompressionLZ4
	case "zstd":
		saramaCfg.Producer.Compression = sarama.CompressionZSTD
	default:
		saramaCfg.Producer.Compression = sarama.CompressionNone
	}

	syncProducer, err := sarama.NewSyncProducer(cfg.Kafka.Brokers, saramaCfg)
	if err != nil {
		return nil, fmt.Errorf("sarama.NewSyncProducer: %w", err)
	}

	return syncProducer, nil
}
