package gen

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	sharedProducer "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/producer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	outbox "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/kafka"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/cursor"
	outboxStore "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/outbox"
	spotStore "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/spot"
	spotCache "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/producer"
	spotService "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"
)

var KafkaProviders = fx.Options(
	fx.Provide(
		provideSaramaSyncProducer,
		provideSpotOutboxStore,
		provideMarketCursorStore,
		provideMarketStateChangedProducer,
		provideSpotOutboxWorker,
		provideMarketEventProducer,
		provideMarketPoller,
	),
)

func providePostgresPool(ctx context.Context, cfg config.SpotConfig) (*pgxpool.Pool, error) {
	pool, err := db.SetupDBWithPoolConfig(ctx, cfg.Service.DBURI, migrations.Migrations, db.PoolConfig{
		MaxConnections:  cfg.PostgresPool.MaxConnections,
		MinConnections:  cfg.PostgresPool.MinConnections,
		MaxConnLifetime: cfg.PostgresPool.MaxConnLifetime,
		MaxConnIdleTime: cfg.PostgresPool.MaxConnIdleTime,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres.SetupDB: %w", err)
	}

	return pool, nil
}

func provideRedisClient(ctx context.Context, cfg config.SpotConfig) (*redis.Client, error) {
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

	pingCtx, cancel := context.WithTimeout(ctx, cfg.Redis.ConnectionTimeout)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis.Ping: %w", err)
	}

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

func provideMarketStateChangedProducer(
	syncProducer sarama.SyncProducer,
	cfg config.SpotConfig,
	logger *zapLogger.Logger,
) outbox.EventPublisher {
	return sharedProducer.New(
		syncProducer,
		cfg.Kafka.Topics.MarketStateChanged,
		cfg.Service.Name,
		logger,
	)
}

func provideSpotOutboxWorker(
	store *outboxStore.OutboxStore,
	publisher outbox.EventPublisher,
	cfg config.SpotConfig,
	logger *zapLogger.Logger,
) *outbox.Worker {
	return outbox.NewWorker(
		store,
		publisher,
		cfg.Kafka.Outbox.PollInterval,
		cfg.Kafka.Outbox.BatchSize,
		cfg.Kafka.Outbox.BatchTimeout,
		cfg.Kafka.Outbox.MaxRetries,
		logger,
		cfg,
	)
}

func provideMarketEventProducer(
	store *outboxStore.OutboxStore,
	cursorStore *cursor.Store,
	logger *zapLogger.Logger,
	cfg config.SpotConfig,
) *producer.MarketProducer {
	return producer.New(store, cursorStore, logger, cfg)
}

func provideMarketPoller(
	store *spotStore.MarketStore,
	marketViewer *spotService.MarketViewer,
	marketProducer *producer.MarketProducer,
	cursorStore *cursor.Store,
	cfg config.SpotConfig,
	logger *zapLogger.Logger,
) *spotService.MarketPoller {
	return spotService.NewMarketPoller(
		store,
		marketProducer,
		cursorStore,
		marketViewer,
		cfg.MarketPoller.PollInterval,
		cfg.MarketPoller.ProcessingTimeout,
		cfg.MarketPoller.BatchSize,
		logger,
	)
}

func RegisterMarketPoller(
	lifecycle fx.Lifecycle,
	appCtx context.Context,
	poller *spotService.MarketPoller,
	logger *zapLogger.Logger,
) {
	ctx, cancel := context.WithCancel(appCtx)
	done := make(chan struct{})

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			logger.Info(startCtx, "Market poller: starting")

			go func() {
				defer close(done)
				poller.Run(ctx)
			}()

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			logger.Info(stopCtx, "Market poller: stopping")
			cancel()

			select {
			case <-done:
				logger.Info(stopCtx, "Market poller: stopped")
				return nil
			case <-stopCtx.Done():
				logger.Warn(stopCtx, "Market poller: stop timeout exceeded", zap.Error(stopCtx.Err()))
				return stopCtx.Err()
			}
		},
	})
}

func provideSpotService(
	repository *spotStore.MarketStore,
	cacheRepository *spotCache.MarketCacheRepository,
	cfg config.SpotConfig,
	logger *zapLogger.Logger,
) *spotService.MarketViewer {
	return spotService.NewMarketViewer(
		repository,
		cacheRepository,
		cfg.Redis.CacheTTL,
		cfg.Timeouts.Service,
		logger,
	)
}

func RegisterOutboxWorker(
	lifecycle fx.Lifecycle,
	appCtx context.Context,
	worker *outbox.Worker,
	logger *zapLogger.Logger,
) {
	ctx, cancel := context.WithCancel(appCtx)
	done := make(chan struct{})

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			logger.Info(startCtx, "Outbox worker: starting")

			go func() {
				defer close(done)
				worker.Run(ctx)
			}()

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			logger.Info(stopCtx, "Outbox worker: stopping")
			cancel()

			select {
			case <-done:
				logger.Info(stopCtx, "Outbox worker: stopped")
				return nil
			case <-stopCtx.Done():
				logger.Warn(stopCtx, "Outbox worker: stop timeout exceeded", zap.Error(stopCtx.Err()))
				return stopCtx.Err()
			}
		},
	})
}

func RegisterKafkaProducer(
	lifecycle fx.Lifecycle,
	syncProducer sarama.SyncProducer,
	logger *zapLogger.Logger,
) {
	lifecycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			logger.Info(ctx, "Kafka producer: stopping")

			if err := syncProducer.Close(); err != nil {
				logger.Error(ctx, "Failed to close sync producer", zap.Error(err))
				return err
			}

			return nil
		},
	})
}
