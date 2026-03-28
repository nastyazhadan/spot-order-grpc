package order

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	outbox "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/kafka"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/consumer"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	metricInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

var Lifecycle = fx.Options(
	fx.Provide(
		provideTracingResource,
	),
	fx.Invoke(
		registerInfrastructure,

		registerTracing,
		registerMetrics,

		registerKafkaProducer,
		registerOutboxWorker,
		registerKafkaConsumer,
	),
)

type appCtxIn struct {
	fx.In
	AppCtx context.Context `name:"app_ctx"`
}

func registerInfrastructure(
	lifecycle fx.Lifecycle,
	cfg config.OrderConfig,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	connection *grpc.ClientConn,
	logger *zapLogger.Logger,
) {
	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			if err := db.BootstrapPostgres(startCtx, pool, migrations.Migrations); err != nil {
				return fmt.Errorf("bootstrap postgres: %w", err)
			}

			redisPingCtx, cancel := context.WithTimeout(startCtx, cfg.Redis.ConnectionTimeout)
			defer cancel()

			if err := redisClient.Ping(redisPingCtx).Err(); err != nil {
				return fmt.Errorf("redis.Ping: %w", err)
			}

			checkCtx, cancel := context.WithTimeout(startCtx, cfg.Timeouts.Check)
			defer cancel()

			if err := health.CheckHealth(checkCtx, connection); err != nil {
				return fmt.Errorf("spot connection at %s health check: %w", cfg.SpotAddress, err)
			}

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			pool.Close()

			if err := redisClient.Close(); err != nil {
				logger.Error(stopCtx, "failed to close redis", zap.Error(err))
			}
			if err := connection.Close(); err != nil {
				logger.Error(stopCtx, "failed to close spot grpc connection", zap.Error(err))
			}

			return nil
		},
	})
}

func provideTracingResource(cfg config.OrderConfig) (*resource.Resource, error) {
	return tracing.NewResource(context.Background(), cfg.Service.Name, cfg.Tracing)
}

func registerTracing(
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
	resource *resource.Resource,
	logger *zapLogger.Logger,
) {
	lifeCycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			return tracing.InitTracer(startCtx, cfg.Tracing, resource)
		},
		OnStop: func(stopCtx context.Context) error {
			if err := tracing.ShutdownTracer(stopCtx); err != nil {
				logger.Error(stopCtx, "Failed to shutdown tracer", zap.Error(err))
			}
			return nil
		},
	})
}

func registerMetrics(
	in appCtxIn,
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
	resource *resource.Resource,
	logger *zapLogger.Logger,
) {
	appCtx := in.AppCtx
	var meterProvider *sdkmetric.MeterProvider

	httpServer := &http.Server{
		Addr: cfg.Metrics.HTTPAddress,
		Handler: promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{EnableOpenMetrics: true},
		),
		ReadTimeout:  cfg.Metrics.ReadTimeout,
		WriteTimeout: cfg.Metrics.WriteTimeout,
		IdleTimeout:  cfg.Metrics.IdleTimeout,
	}

	lifeCycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			var err error
			meterProvider, err = metricInterceptor.InitOpenTelemetry(startCtx, cfg.Metrics, resource, logger)
			if err != nil {
				return err
			}

			go func() {
				if startErr := httpServer.ListenAndServe(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
					logger.Error(appCtx, "Failed to start metrics server", zap.Error(startErr))
				}
			}()
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			metrics.RecordShutdown(cfg.Service.Name)

			if meterProvider != nil {
				if stopErr := meterProvider.Shutdown(stopCtx); stopErr != nil {
					logger.Error(stopCtx, "Failed to shutdown metrics provider", zap.Error(stopErr))
				}
			}
			return httpServer.Shutdown(stopCtx)
		},
	})
}

func registerKafkaProducer(
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

func registerOutboxWorker(
	lifecycle fx.Lifecycle,
	in appCtxIn,
	worker *outbox.Worker,
	logger *zapLogger.Logger,
) {
	appCtx := in.AppCtx
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

func registerKafkaConsumer(
	lifecycle fx.Lifecycle,
	in appCtxIn,
	consumer *consumer.MarketConsumer,
	group sarama.ConsumerGroup,
	logger *zapLogger.Logger,
) {
	appCtx := in.AppCtx
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
