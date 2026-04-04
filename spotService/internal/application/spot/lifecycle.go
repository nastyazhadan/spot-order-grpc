package spot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

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
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	metricInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
	outbox "github.com/nastyazhadan/spot-order-grpc/spotService/internal/infrastructure/kafka"
	spotService "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
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

		registerReadiness,
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
	var (
		meterProvider *sdkmetric.MeterProvider
		listener      net.Listener
	)

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

			listener, err = net.Listen("tcp", cfg.Metrics.HTTPAddress)
			if err != nil {
				return fmt.Errorf("listen metrics http on %s: %w", cfg.Metrics.HTTPAddress, err)
			}

			go func() {
				if serveErr := httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					logger.Error(appCtx, "Failed to serve metrics server", zap.Error(serveErr))
				}
			}()
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			var shutdownErr error

			metrics.RecordShutdown(cfg.Service.Name, metrics.ShutdownReasonFromContext(stopCtx))

			timer := time.NewTimer(cfg.Metrics.ShutdownTimeout)
			defer timer.Stop()

			select {
			case <-timer.C:
			case <-stopCtx.Done():
				logger.Warn(stopCtx, "Shutdown metrics interrupted by stop context", zap.Error(stopCtx.Err()))
			}

			if err := httpServer.Shutdown(stopCtx); err != nil {
				logger.Error(stopCtx, "Failed to shutdown metrics server", zap.Error(err))
				shutdownErr = errors.Join(shutdownErr, err)
			}

			if meterProvider != nil {
				if err := meterProvider.Shutdown(stopCtx); err != nil {
					logger.Error(stopCtx, "Failed to shutdown metrics provider", zap.Error(err))
					shutdownErr = errors.Join(shutdownErr, err)
				}
			}

			return shutdownErr
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

	var (
		workerCtx context.Context
		cancel    context.CancelFunc
		done      chan struct{}
	)

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			workerCtx, cancel = context.WithCancel(appCtx)
			done = make(chan struct{})

			logger.Info(startCtx, "Spot outbox worker: starting")

			go func() {
				defer close(done)

				err := recovery.PanicRecoveryHandler(workerCtx, logger, "Spot outbox worker",
					func() error {
						return worker.Run(workerCtx)
					},
				)
				if err != nil && workerCtx.Err() == nil {
					logger.Error(workerCtx, "Spot outbox worker stopped with error", zap.Error(err))
				}
			}()

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			logger.Info(stopCtx, "Spot outbox worker: stopping")
			cancel()

			select {
			case <-done:
				logger.Info(stopCtx, "Spot outbox worker: stopped")
				return nil
			case <-stopCtx.Done():
				logger.Warn(stopCtx, "Spot outbox worker: stop timeout exceeded", zap.Error(stopCtx.Err()))
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
	config config.SpotConfig,
) {
	appCtx := in.AppCtx

	var (
		workerCtx context.Context
		cancel    context.CancelFunc
		done      chan struct{}
	)

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			workerCtx, cancel = context.WithCancel(appCtx)
			done = make(chan struct{})

			logger.Info(startCtx, "Market poller: starting")

			if err := poller.Init(startCtx); err != nil {
				logger.Error(startCtx, "Market poller: failed to initialize", zap.Error(err))
				return err
			}

			go func() {
				defer close(done)

				for {
					err := recovery.PanicRecoveryHandler(workerCtx, logger, "Market poller", func() error {
						return poller.Run(workerCtx)
					})
					if err == nil || workerCtx.Err() != nil {
						logger.Info(workerCtx, "Market poller stopped")
						return
					}

					logger.Error(workerCtx, "Market poller exited with error, restarting",
						zap.Error(err),
						zap.Duration("restart_after", config.MarketPoller.RestartBackoff),
					)

					select {
					case <-workerCtx.Done():
						return
					case <-time.After(config.MarketPoller.RestartBackoff):
					}
				}
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

func registerReadiness(
	lifecycle fx.Lifecycle,
	healthServer *health.Server,
) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			healthServer.SetServing()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			healthServer.SetNotServing()
			return nil
		},
	})
}
