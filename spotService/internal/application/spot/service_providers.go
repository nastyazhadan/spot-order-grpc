package spot

import (
	"github.com/IBM/sarama"
	"go.uber.org/fx"

	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	sharedProducer "github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/producer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	outbox "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/kafka"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/cursor"
	outboxStore "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/outbox"
	spotStore "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/postgres/spot"
	spotCache "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/redis"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/producer"
	spotService "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
)

var ServiceProviders = fx.Options(
	fx.Provide(
		provideJWTManager,

		provideKafkaClient,
		provideMarketStateChangedProducer,
		provideSpotOutboxWorker,
		provideMarketEventProducer,

		provideSpotService,
		provideMarketPoller,
		provideContainer,
	),
)

type container struct {
	JWTManager  *authjwt.Manager
	SpotService *spotService.MarketViewer
}

func provideJWTManager(cfg config.SpotConfig) *authjwt.Manager {
	return authjwt.NewManager(cfg.AuthVerifier.JWTSecret, 0, 0)
}

func provideKafkaClient(
	asyncProducer sarama.AsyncProducer,
	cfg config.SpotConfig,
	logger *zapLogger.Logger,
) *sharedProducer.Client {
	return sharedProducer.NewClient(
		asyncProducer,
		cfg.Service.Name,
		logger,
	)
}

func provideMarketStateChangedProducer(
	client *sharedProducer.Client,
	cfg config.SpotConfig,
) outbox.EventPublisher {
	return sharedProducer.New(
		client,
		cfg.Kafka.Topics.MarketStateChanged,
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

func provideSpotService(
	repository *spotStore.MarketStore,
	cacheRepository *spotCache.MarketCacheRepository,
	cacheByIDRepository *spotCache.MarketByIDCacheRepository,
	cfg config.SpotConfig,
	logger *zapLogger.Logger,
) *spotService.MarketViewer {
	return spotService.NewMarketViewer(
		repository,
		cacheRepository,
		cacheByIDRepository,
		cfg.Redis.CacheTTL,
		cfg.Timeouts.Service,
		cfg.ViewMarkets.DefaultLimit,
		cfg.ViewMarkets.MaxLimit,
		cfg.ViewMarkets.CacheLimit,
		cfg.Service.Name,
		logger,
	)
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

func provideContainer(
	jwtManager *authjwt.Manager,
	service *spotService.MarketViewer,
) *container {
	return &container{
		JWTManager:  jwtManager,
		SpotService: service,
	}
}
