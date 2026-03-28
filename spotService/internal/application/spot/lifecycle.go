package spot

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

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	metricInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	outbox "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/kafka"
	spotService "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"
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
		registerMarketPoller,
	),
)

type appCtxIn struct {
	fx.In
	AppCtx context.Context `name:"app_ctx"`
}

func registerInfrastructure(
	lifecycle fx.Lifecycle,
	cfg config.SpotConfig,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
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

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			pool.Close()

			if err := redisClient.Close(); err != nil {
				logger.Error(stopCtx, "failed to close redis", zap.Error(err))
			}

			return nil
		},
	})
}

func provideTracingResource(cfg config.SpotConfig) (*resource.Resource, error) {
	return tracing.NewResource(context.Background(), cfg.Service.Name, cfg.Tracing)
}

func registerTracing(
	lifeCycle fx.Lifecycle,
	cfg config.SpotConfig,
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
	cfg config.SpotConfig,
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

func registerMarketPoller(
	lifecycle fx.Lifecycle,
	in appCtxIn,
	poller *spotService.MarketPoller,
	logger *zapLogger.Logger,
) {
	appCtx := in.AppCtx
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
