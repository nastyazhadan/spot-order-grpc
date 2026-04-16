package order

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	outbox "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/kafka"
	inboxStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/inbox"
	orderStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/order"
	outboxStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/postgres/outbox"
	authStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/auth"
	idemStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/idempotency"
	blockStore "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/market"
	orderCache "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/redis/order"
	authService "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/auth"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/consumer"
	orderService "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/producer"
	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	authsession "github.com/nastyazhadan/spot-order-grpc/shared/auth/session"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
	sharedConsumer "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/consumer"
	sharedProducer "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/producer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
)

const (
	prefixCreateLimiter = "rate:order:create:"
	prefixGetLimiter    = "rate:order:get:"
	middlewaresCount    = 2
)

var ServiceProviders = fx.Options(
	fx.Provide(
		provideRateLimiters,

		provideJWTManager,
		provideRefreshTokenStore,
		provideSessionStore,
		provideAuthService,

		provideKafkaClient,
		provideKafkaPublisher,
		provideDLQPublisher,
		provideEventProducer,

		provideOutboxWorker,
		provideCompensationService,
		provideConsumerService,

		provideIdempotencyService,
		provideOrderService,
		provideContainer,
	),
)

type container struct {
	JWTManager        *authjwt.Manager
	SessionStore      *authsession.Store
	RefreshTokenStore *authStore.RefreshTokenStore
	AuthService       *authService.AuthService
	OrderService      *orderService.OrderService
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
		cfg.AuthVerifier.JWTSecret,
		cfg.AuthIssuer.AccessTokenTTL,
		cfg.AuthIssuer.RefreshTokenTTL,
	)
}

func provideRefreshTokenStore(store *cache.Store, cfg config.OrderConfig) *authStore.RefreshTokenStore {
	return authStore.New(store, cfg.AuthIssuer.RefreshTokenTTL)
}

func provideSessionStore(store *cache.Store) *authsession.Store {
	return authsession.New(store)
}

func provideAuthService(
	jwtManager *authjwt.Manager,
	refreshStore *authStore.RefreshTokenStore,
	sessionStore *authsession.Store,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) *authService.AuthService {
	return authService.New(jwtManager, refreshStore, sessionStore, cfg.Timeouts.Service, logger)
}

func provideKafkaClient(
	asyncProducer sarama.AsyncProducer,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) *sharedProducer.Client {
	return sharedProducer.NewClient(
		asyncProducer,
		cfg.Service.Name,
		logger,
	)
}

func provideKafkaPublisher(
	client *sharedProducer.Client,
	cfg config.OrderConfig,
) outbox.EventPublisher {
	return sharedProducer.New(
		client,
		cfg.Kafka.Topics.OrderCreated,
	)
}

func provideDLQPublisher(
	client *sharedProducer.Client,
	cfg config.OrderConfig,
) sharedConsumer.DLQPublisher {
	if !cfg.Kafka.Consumer.DLQEnabled {
		return nil
	}

	return sharedProducer.New(
		client,
		cfg.Kafka.Topics.MarketStateChangedDLQ,
	)
}

func provideIdempotencyService(
	store *cache.Store,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) *orderService.IdempotencyService {
	idempotencyStore := idemStore.New(store, cfg.Redis.Idempotency.RequestTTL)
	adapter := &redisIdempotencyAdapter{store: idempotencyStore}

	return orderService.NewIdempotencyService(adapter, logger, cfg)
}

func provideOrderService(
	lifecycle fx.Lifecycle,
	pool *pgxpool.Pool,
	store *orderStore.OrderStore,
	marketViewer orderService.MarketViewer,
	blockStore *blockStore.MarketBlockStore,
	rateLimiters orderService.RateLimiters,
	eventProducer orderService.EventProducer,
	service *orderService.IdempotencyService,
	logger *zapLogger.Logger,
	cfg config.OrderConfig,
) *orderService.OrderService {
	svc := orderService.New(
		pool,
		store,
		store,
		marketViewer,
		blockStore,
		rateLimiters,
		eventProducer,
		service,
		logger,
		cfg,
	)

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return svc.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return svc.Close(ctx)
		},
	})

	return svc
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

func provideEventProducer(store *outboxStore.OutboxStore, logger *zapLogger.Logger) orderService.EventProducer {
	return producer.New(store, logger)
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

func provideConsumerService(
	group sarama.ConsumerGroup,
	service *orderService.CompensationService,
	dlqPublisher sharedConsumer.DLQPublisher,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) *consumer.MarketConsumer {
	middlewares := make([]sharedConsumer.Middleware, 0, middlewaresCount)

	if cfg.Kafka.Consumer.DLQEnabled {
		middlewares = append(middlewares,
			sharedConsumer.DLQMiddleware(
				dlqPublisher,
				cfg.Kafka.Consumer.DLQMaxMessageBytes,
				logger,
			),
		)
	}
	middlewares = append(middlewares,
		sharedConsumer.RetryMiddleware(
			cfg.Kafka.Consumer.MaxRetries,
			cfg.Kafka.Consumer.RetryBackoff,
			cfg.Kafka.Consumer.RetryJitter,
			logger,
		),
	)

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

func provideContainer(
	jwtManager *authjwt.Manager,
	sessionStore *authsession.Store,
	tokenStore *authStore.RefreshTokenStore,
	authService *authService.AuthService,
	orderService *orderService.OrderService,
) *container {
	return &container{
		JWTManager:        jwtManager,
		SessionStore:      sessionStore,
		RefreshTokenStore: tokenStore,
		AuthService:       authService,
		OrderService:      orderService,
	}
}
