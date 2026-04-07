package spot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"
	"go.uber.org/zap"

	spotv1 "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/spot/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/kafka/producer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
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
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	logger *zapLogger.Logger,
) {
	lifecycle.Append(fx.Hook{
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
	logger *zapLogger.Logger,
) {
	appCtx := in.AppCtx
	var listener net.Listener

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

			listener, err = net.Listen("tcp", cfg.Metrics.HTTPAddress)
			if err != nil {
				return fmt.Errorf("listen metrics http on %s: %w", cfg.Metrics.HTTPAddress, err)
			}

			go func() {
				if serveError := httpServer.Serve(listener); serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
					logger.Error(appCtx, "Failed to serve metrics server", zap.Error(serveError))
				}
			}()
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(stopCtx), cfg.Metrics.ShutdownTimeout)
			defer cancel()

			metrics.ShutdownsTotal.WithLabelValues(cfg.Service.Name, "graceful").Inc()

			var shutdownError error

			if httpServer != nil {
				if err := httpServer.Shutdown(shutdownCtx); err != nil {
					logger.Error(shutdownCtx, "Failed to shutdown metrics server", zap.Error(err))
					shutdownError = errors.Join(shutdownError, err)
				}
			}

			return shutdownError
		},
	})
}

func registerOutboxWorker(
	in appCtxIn,
	lifecycle fx.Lifecycle,
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
	in appCtxIn,
	lifecycle fx.Lifecycle,
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
	client *producer.Client,
	logger *zapLogger.Logger,
) {
	lifecycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			logger.Info(ctx, "Kafka producer: stopping")

			if err := client.Close(); err != nil {
				logger.Error(ctx, "Failed to close Kafka producer", zap.Error(err))
				return err
			}

			return nil
		},
	})
}

func registerReadiness(
	in appCtxIn,
	lifecycle fx.Lifecycle,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	healthServer *health.Server,
	logger *zapLogger.Logger,
	cfg config.SpotConfig,
) {
	appCtx := in.AppCtx

	var (
		watchCtx context.Context
		cancel   context.CancelFunc
		done     chan struct{}
	)

	monitor := buildReadinessMonitor(cfg, pool, healthServer, redisClient, logger)

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			if err := monitor.RequireReady(startCtx); err != nil {
				logger.Error(startCtx, "spot readiness check failed", zap.Error(err))
				return err
			}

			watchCtx, cancel = context.WithCancel(appCtx)
			done = make(chan struct{})

			go func() {
				defer close(done)
				monitor.Watch(watchCtx)
			}()

			logger.Info(startCtx, "Spot service is ready")
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			if cancel != nil {
				cancel()
			}

			if done != nil {
				select {
				case <-done:
				case <-stopCtx.Done():
					return stopCtx.Err()
				}
			}

			healthServer.Shutdown()
			return nil
		},
	})
}

func buildReadinessMonitor(
	cfg config.SpotConfig,
	pool *pgxpool.Pool,
	healthServer *health.Server,
	redisClient *redis.Client,
	logger *zapLogger.Logger,
) *health.ReadinessMonitor {
	monitor := health.NewReadinessMonitor(
		healthServer,
		logger,
		health.ReadinessMonitorConfig{
			ServiceName:      cfg.Service.Name,
			ServiceNames:     []string{spotv1.SpotInstrumentService_ServiceDesc.ServiceName},
			CheckInterval:    cfg.Health.CheckInterval,
			CheckTimeout:     cfg.Timeouts.Check,
			SuccessThreshold: cfg.Health.SuccessThreshold,
			FailureThreshold: cfg.Health.FailureThreshold,
		},
		health.DependencyCheck{
			Name: "postgres",
			Check: func(ctx context.Context) error {
				return pool.Ping(ctx)
			},
		},
		health.DependencyCheck{
			Name: "redis",
			Check: func(ctx context.Context) error {
				return redisClient.Ping(ctx).Err()
			},
		},
	)

	return monitor
}
