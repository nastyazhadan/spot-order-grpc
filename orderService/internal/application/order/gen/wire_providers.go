package gen

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"go.uber.org/zap"

	outbox "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/kafka"
	inboxStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/inbox"
	orderStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/order"
	outboxStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/outbox"
	authStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/auth"
	blockStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/market"
	orderCache "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/order"
	authService "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/auth"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/consumer"
	orderService "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/producer"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	sharedConsumer "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/consumer"
	sharedProducer "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/producer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

const (
	prefixCreateLimiter = "rate:order:create:"
	prefixGetLimiter    = "rate:order:get:"
)

var KafkaProviders = fx.Options(
	fx.Provide(
		provideSaramaSyncProducer,
		provideKafkaPublisher,
		provideDLQPublisher,

		provideOutboxStore,
		provideInboxStore,
		provideOutboxWorker,
		provideEventProducer,

		provideConsumerGroup,
		provideCompensationService,
		provideConsumerService,
		provideBlockStore,
	),
)

func providePostgresPool(ctx context.Context, cfg config.OrderConfig) (*pgxpool.Pool, error) {
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

func provideRedisClient(ctx context.Context, cfg config.OrderConfig) (*redis.Client, error) {
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

func provideOrderStore(pool *pgxpool.Pool, cfg config.OrderConfig) *orderStore.OrderStore {
	return orderStore.New(pool, cfg)
}

func provideOutboxStore(pool *pgxpool.Pool, logger *zapLogger.Logger, cfg config.OrderConfig) *outboxStore.OutboxStore {
	return outboxStore.New(pool, logger, cfg)
}

func provideBlockStore(store *cache.Store) *blockStore.MarketBlockStore {
	return blockStore.New(store)
}

func provideEventProducer(store *outboxStore.OutboxStore, logger *zapLogger.Logger) orderService.EventProducer {
	return producer.New(store, logger)
}

func provideInboxStore(pool *pgxpool.Pool, cfg config.OrderConfig) *inboxStore.InboxStore {
	return inboxStore.New(pool, cfg)
}

func provideRateLimiters(store *cache.Store, cfg config.OrderConfig) orderService.RateLimiters {
	return orderService.RateLimiters{
		Create: orderCache.NewOrderRateLimiter(
			store,
			cfg.RateLimitByUser.CreateOrder,
			cfg.RateLimitByUser.Window,
			prefixCreateLimiter,
		),
		Get: orderCache.NewOrderRateLimiter(
			store,
			cfg.RateLimitByUser.GetOrderStatus,
			cfg.RateLimitByUser.Window,
			prefixGetLimiter,
		),
	}
}

func provideJWTManager(cfg config.OrderConfig) *authjwt.Manager {
	return authjwt.NewManager(
		cfg.Auth.JWTSecret,
		cfg.Auth.AccessTokenTTL,
		cfg.Auth.RefreshTokenTTL,
	)
}

func provideRefreshTokenStore(store *cache.Store, cfg config.OrderConfig) *authStore.RefreshTokenStore {
	return authStore.New(store, cfg.Auth.RefreshTokenTTL)
}

func provideAuthService(
	jwtManager *authjwt.Manager,
	store *authStore.RefreshTokenStore,
	logger *zapLogger.Logger,
) *authService.AuthService {
	return authService.New(jwtManager, store, logger)
}

func provideOrderServiceConfig(cfg config.OrderConfig) orderService.Config {
	return orderService.Config{
		Timeout:     cfg.Timeouts.Service,
		ServiceName: cfg.Service.Name,
	}
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

func provideKafkaPublisher(
	syncProducer sarama.SyncProducer,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) outbox.EventPublisher {
	return sharedProducer.New(
		syncProducer,
		cfg.Kafka.Topics.OrderCreated,
		cfg.Service.Name,
		logger,
	)
}

func provideDLQPublisher(
	syncProducer sarama.SyncProducer,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) sharedConsumer.DLQPublisher {
	return sharedProducer.New(
		syncProducer,
		cfg.Kafka.Topics.MarketStateChangedDLQ,
		cfg.Service.Name,
		logger,
	)
}

func provideOutboxWorker(
	store *outboxStore.OutboxStore,
	publisher outbox.EventPublisher,
	cfg config.OrderConfig,
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

func provideConsumerService(
	group sarama.ConsumerGroup,
	service *orderService.CompensationService,
	dlqPublisher sharedConsumer.DLQPublisher,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) *consumer.MarketConsumer {
	middlewares := make([]sharedConsumer.Middleware, 0, 1)

	if cfg.Kafka.Consumer.DLQEnabled {
		middlewares = append(middlewares, sharedConsumer.RetryWithDLQMiddleware(
			cfg.Kafka.Consumer.MaxRetries,
			cfg.Kafka.Consumer.RetryBackoff,
			cfg.Service.Name,
			dlqPublisher,
			logger,
		))
	}

	kafkaConsumer := sharedConsumer.New(
		group,
		[]string{cfg.Kafka.Topics.MarketStateChanged},
		cfg.Service.Name,
		logger,
		middlewares...,
	)

	return consumer.NewMarketConsumer(
		kafkaConsumer,
		service,
		cfg.Kafka.Consumer.GroupID,
		logger,
	)
}

func provideCompensationService(
	pool *pgxpool.Pool,
	orderStore *orderStore.OrderStore,
	inboxStore *inboxStore.InboxStore,
	blockStore *blockStore.MarketBlockStore,
	eventProducer orderService.EventProducer,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *orderService.CompensationService {
	return orderService.NewCompensationService(
		pool,
		inboxStore,
		orderStore,
		blockStore,
		eventProducer,
		logger,
		cfg,
	)
}

func provideOrderService(
	pool *pgxpool.Pool,
	store *orderStore.OrderStore,
	marketViewer orderService.MarketViewer,
	blockStore *blockStore.MarketBlockStore,
	rateLimiters orderService.RateLimiters,
	cfg orderService.Config,
	eventProducer orderService.EventProducer,
	logger *zapLogger.Logger,
) *orderService.OrderService {
	return orderService.New(
		pool,
		store,
		store,
		marketViewer,
		blockStore,
		rateLimiters,
		cfg,
		eventProducer,
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

func RegisterKafkaConsumer(
	lifecycle fx.Lifecycle,
	appCtx context.Context,
	consumer *consumer.MarketConsumer,
	group sarama.ConsumerGroup,
	logger *zapLogger.Logger,
) {
	ctx, cancel := context.WithCancel(appCtx)
	done := make(chan struct{})

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			logger.Info(startCtx, "Kafka consumer: starting")
			go func() {
				defer close(done)

				if err := consumer.Run(ctx); err != nil {
					if ctx.Err() != nil {
						logger.Info(appCtx, "Kafka consumer stopped")
						return
					}
					logger.Error(appCtx, "Kafka consumer exited with error", zap.Error(err))
				}
			}()

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			logger.Info(stopCtx, "Kafka consumer: stopping")
			cancel()

			if err := group.Close(); err != nil {
				logger.Error(stopCtx, "Failed to close consumer group", zap.Error(err))
			}

			select {
			case <-done:
				logger.Info(stopCtx, "Kafka consumer: stopped")
				return nil
			case <-stopCtx.Done():
				logger.Warn(stopCtx, "Kafka consumer: stop timeout exceeded", zap.Error(stopCtx.Err()))
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
			}

			return nil
		},
	})
}
