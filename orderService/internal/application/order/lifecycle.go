package order

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
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	outbox "github.com/nastyazhadan/spot-order-grpc/orderService/internal/infrastructure/kafka"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/consumer"
	authv1 "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/auth/v1"
	orderv1 "github.com/nastyazhadan/spot-order-grpc/protos/gen/go/order/v1"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
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

		registerReadiness,
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
			if err := connection.Close(); err != nil {
				logger.Error(stopCtx, "failed to close spot gRPC connection", zap.Error(err))
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

	var (
		workerCtx context.Context
		cancel    context.CancelFunc
		done      chan struct{}
	)

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			workerCtx, cancel = context.WithCancel(appCtx)
			done = make(chan struct{})

			logger.Info(startCtx, "Outbox worker: starting")

			go func() {
				defer close(done)

				err := recovery.PanicRecoveryHandler(workerCtx, logger, "Outbox worker",
					func() error {
						return worker.Run(workerCtx)
					},
				)
				if err != nil && workerCtx.Err() == nil {
					logger.Error(workerCtx, "Outbox worker stopped with error", zap.Error(err))
				}
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
	config config.OrderConfig,
) {
	appCtx := in.AppCtx

	var (
		consumerCtx context.Context
		cancel      context.CancelFunc
		done        chan struct{}
	)

	lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			consumerCtx, cancel = context.WithCancel(appCtx)
			done = make(chan struct{})

			logger.Info(startCtx, "Kafka consumer: starting")

			go func() {
				defer close(done)

				for {
					err := recovery.PanicRecoveryHandler(consumerCtx, logger, "Kafka consumer",
						func() error {
							return consumer.Run(consumerCtx)
						},
					)
					if err == nil || consumerCtx.Err() != nil {
						logger.Info(consumerCtx, "Kafka consumer stopped")
						return
					}

					logger.Error(consumerCtx, "Kafka consumer exited with error, restarting",
						zap.Error(err),
						zap.Duration("restart_after", config.Kafka.Consumer.RestartBackoff),
					)

					select {
					case <-consumerCtx.Done():
						return
					case <-time.After(config.Kafka.Consumer.RestartBackoff):
					}
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

func registerReadiness(
	lifecycle fx.Lifecycle,
	healthServer *health.Server,
) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			healthServer.SetServingStatus(
				"",
				grpc_health_v1.HealthCheckResponse_SERVING)
			healthServer.SetServingStatus(
				orderv1.OrderService_ServiceDesc.ServiceName,
				grpc_health_v1.HealthCheckResponse_SERVING,
			)
			healthServer.SetServingStatus(
				authv1.AuthService_ServiceDesc.ServiceName,
				grpc_health_v1.HealthCheckResponse_SERVING,
			)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			healthServer.Shutdown()
			return nil
		},
	})
}
