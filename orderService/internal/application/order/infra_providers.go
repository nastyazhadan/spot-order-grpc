package order

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"

	inboxStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/inbox"
	orderStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/order"
	outboxStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/outbox"
	blockStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/market"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

var InfraProviders = fx.Options(
	fx.Provide(
		provideLogger,

		providePostgresPool,
		provideRedisClient,
		provideCacheStore,

		provideOrderStore,
		provideOutboxStore,
		provideInboxStore,
		provideBlockStore,

		provideSaramaSyncProducer,
		provideConsumerGroup,
	),
)

func provideLogger(lifeCycle fx.Lifecycle, cfg config.OrderConfig) (*zapLogger.Logger, error) {
	logger := zapLogger.New(cfg.Log.Level, cfg.Log.Format == "json")

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return logger.Sync()
		},
	})

	return logger, nil
}

func providePostgresPool(cfg config.OrderConfig) (*pgxpool.Pool, error) {
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

func provideRedisClient(cfg config.OrderConfig) (*redis.Client, error) {
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

func provideOrderStore(pool *pgxpool.Pool, cfg config.OrderConfig) *orderStore.OrderStore {
	return orderStore.New(pool, cfg)
}

func provideOutboxStore(pool *pgxpool.Pool, logger *zapLogger.Logger, cfg config.OrderConfig) *outboxStore.OutboxStore {
	return outboxStore.New(pool, logger, cfg)
}

func provideBlockStore(store *cache.Store) *blockStore.MarketBlockStore {
	return blockStore.New(store)
}

func provideInboxStore(pool *pgxpool.Pool, cfg config.OrderConfig) *inboxStore.InboxStore {
	return inboxStore.New(pool, cfg)
}

func provideSaramaSyncProducer(cfg config.OrderConfig) (sarama.SyncProducer, error) {
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

func provideConsumerGroup(cfg config.OrderConfig) (sarama.ConsumerGroup, error) {
	saramaCfg := sarama.NewConfig()

	saramaCfg.Consumer.Group.Session.Timeout = cfg.Kafka.Consumer.SessionTimeout
	saramaCfg.Consumer.Group.Heartbeat.Interval = cfg.Kafka.Consumer.HeartbeatInterval
	saramaCfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	group, err := sarama.NewConsumerGroup(cfg.Kafka.Brokers, cfg.Kafka.Consumer.GroupID, saramaCfg)
	if err != nil {
		return nil, fmt.Errorf("sarama.NewConsumerGroup: %w", err)
	}

	return group, nil
}
