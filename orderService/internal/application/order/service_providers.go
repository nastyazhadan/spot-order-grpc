package order

import (
	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

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
	middlewaresCount    = 3
)

var ServiceProviders = fx.Options(
	fx.Provide(
		provideRateLimiters,

		provideJWTManager,
		provideRefreshTokenStore,
		provideSessionStore,
		provideAuthService,
		provideOrderServiceConfig,

		provideKafkaPublisher,
		provideDLQPublisher,
		provideEventProducer,
		provideOutboxWorker,
		provideCompensationService,
		provideConsumerService,

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
		cfg.Auth.JWTSecret,
		cfg.Auth.AccessTokenTTL,
		cfg.Auth.RefreshTokenTTL,
	)
}

func provideRefreshTokenStore(store *cache.Store, cfg config.OrderConfig) *authStore.RefreshTokenStore {
	return authStore.New(store, cfg.Auth.RefreshTokenTTL)
}

func provideSessionStore(store *cache.Store) *authsession.Store {
	return authsession.New(store)
}

func provideAuthService(
	jwtManager *authjwt.Manager,
	refreshStore *authStore.RefreshTokenStore,
	sessionStore *authsession.Store,
	logger *zapLogger.Logger,
) *authService.AuthService {
	return authService.New(jwtManager, refreshStore, sessionStore, logger)
}

func provideOrderServiceConfig(cfg config.OrderConfig) orderService.Config {
	return orderService.Config{
		Timeout:     cfg.Timeouts.Service,
		ServiceName: cfg.Service.Name,
	}
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
	if !cfg.Kafka.Consumer.DLQEnabled {
		return nil
	}

	return sharedProducer.New(
		syncProducer,
		cfg.Kafka.Topics.MarketStateChangedDLQ,
		cfg.Service.Name,
		logger,
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
