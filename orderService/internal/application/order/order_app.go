package order

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	wireGen "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order/gen"
	grpcAuth "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/auth"
	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	orderService "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/auth"
	grpcErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	metricInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/ratelimit"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/metrics"
)

func Run(ctx context.Context, cfg config.OrderConfig) {
	app := fx.New(
		fx.Provide(
			func() context.Context {
				return ctx
			},
			func() config.OrderConfig {
				return cfg
			}),
		fx.Provide(
			provideLogger,
			provideTracingResource,
			provideClientConnection,
			provideGRPCClient,
			provideContainer,
			provideListener,
			provideGRPCServer,
		),
		wireGen.KafkaProviders,
		fx.Invoke(
			registerTracing,
			registerMetrics,
			startGRPCServer,
			wireGen.RegisterOutboxWorker,
			wireGen.RegisterKafkaConsumer,
		),
	)

	app.Run()
}

func provideLogger(lifeCycle fx.Lifecycle, cfg config.OrderConfig) (*zapLogger.Logger, error) {
	logger := zapLogger.New(cfg.Log.Level, cfg.Log.Format == "json")

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return logger.Sync()
		},
	})

	return logger, nil
}

func provideTracingResource(ctx context.Context, cfg config.OrderConfig) (*resource.Resource, error) {
	return tracing.NewResource(ctx, cfg.Service.Name, cfg.Tracing)
}

func registerTracing(
	ctx context.Context,
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
	resource *resource.Resource,
	logger *zapLogger.Logger,
) error {
	if err := tracing.InitTracer(ctx, cfg.Tracing, resource); err != nil {
		return err
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			if err := tracing.ShutdownTracer(ctx); err != nil {
				logger.Error(ctx, "Failed to shutdown tracer", zap.Error(err))
			}
			return nil
		},
	})

	return nil
}

func registerMetrics(
	ctx context.Context,
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
	resource *resource.Resource,
	logger *zapLogger.Logger,
) error {
	meterProvider, err := metricInterceptor.InitOpenTelemetry(ctx, cfg.Metrics, resource, logger)
	if err != nil {
		return err
	}

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
		OnStart: func(ctx context.Context) error {
			go func() {
				if startErr := httpServer.ListenAndServe(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
					logger.Error(ctx, "Failed to start metrics server", zap.Error(startErr))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			metrics.RecordShutdown(cfg.Service.Name)
			if stopErr := meterProvider.Shutdown(ctx); stopErr != nil {
				logger.Error(ctx, "Failed to shutdown metrics provider", zap.Error(stopErr))
			}
			return httpServer.Shutdown(ctx)
		},
	})

	return nil
}

func provideClientConnection(
	cfg config.OrderConfig,
) (*grpc.ClientConn, error) {
	connection, err := grpc.NewClient(
		cfg.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			tracing.UnaryClientInterceptor(),
			retry.UnaryClientInterceptor(
				retry.WithMax(cfg.Retry.MaxAttempts),
				retry.WithBackoff(retry.BackoffExponentialWithJitter(cfg.Retry.InitialBackoff, cfg.Retry.Jitter)),
				retry.WithCodes(codes.DeadlineExceeded, codes.ResourceExhausted, codes.Unavailable),
				retry.WithPerRetryTimeout(cfg.Retry.PerRetryTimeout),
			),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                cfg.KeepAlive.PingTime,
			Timeout:             cfg.KeepAlive.PingTimeout,
			PermitWithoutStream: cfg.KeepAlive.PermitWithoutStream,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient: %w", err)
	}

	return connection, nil
}

func provideGRPCClient(
	ctx context.Context,
	connection *grpc.ClientConn,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) (*grpcClient.SpotClient, error) {
	client := grpcClient.NewSpotClient(connection, cfg.CircuitBreaker, logger)

	checkCtx, cancel := context.WithTimeout(ctx, cfg.Timeouts.Check)
	defer cancel()

	err := health.CheckHealth(checkCtx, connection)
	if err != nil {
		return nil, fmt.Errorf("spot connection at %s health check: %w", cfg.SpotAddress, err)
	}

	return client, nil
}

func provideContainer(
	ctx context.Context,
	marketClient *grpcClient.SpotClient,
	eventProducer orderService.EventProducer,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) (*wireGen.Container, error) {
	container, err := wireGen.NewContainer(ctx, marketClient, eventProducer, cfg, logger)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func provideListener(
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
) (net.Listener, error) {
	listener, err := net.Listen("tcp", cfg.Service.Address)
	if err != nil {
		return nil, fmt.Errorf("net.Listen: %w", err)
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			errClose := listener.Close()
			if errClose != nil && !errors.Is(errClose, net.ErrClosed) {
				return errClose
			}

			return nil
		},
	})

	return listener, nil
}

func provideGRPCServer(
	container *wireGen.Container,
	cfg config.OrderConfig,
	appLogger *zapLogger.Logger,
) (*grpc.Server, error) {
	validator := validate.UnaryServerInterceptor()
	recoverer := recovery.UnaryServerInterceptor(appLogger)
	tracer := tracing.UnaryServerInterceptor()
	logger := logInterceptor.UnaryServerInterceptor(appLogger)
	authenticator := auth.UnaryServerInterceptor(container.JWTManager, cfg.Auth)
	errorsMapper := grpcErrors.UnaryServerInterceptor(appLogger)
	rateLimiter := ratelimit.OrderUnaryServerInterceptor(cfg, appLogger)
	meter := metricInterceptor.UnaryServerInterceptor(cfg.Service.Name)

	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(cfg.Service.MaxRecvMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    cfg.KeepAlive.PingTime,
			Timeout: cfg.KeepAlive.PingTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             cfg.KeepAlive.MinPingInterval,
			PermitWithoutStream: cfg.KeepAlive.PermitWithoutStream,
		}),
		// logger до auth для логов auth
		grpc.ChainUnaryInterceptor(
			recoverer, tracer, meter, logger, authenticator, rateLimiter, errorsMapper, validator,
		),
	)

	reflection.Register(grpcServer)
	health.RegisterService(grpcServer)
	grpcAuth.Register(grpcServer, container.AuthService)
	grpcOrder.Register(grpcServer, container.OrderService)

	return grpcServer, nil
}

func startGRPCServer(
	lifeCycle fx.Lifecycle,
	server *grpc.Server,
	listener net.Listener,
	container *wireGen.Container,
	connection *grpc.ClientConn,
	logger *zapLogger.Logger,
) {
	lifeCycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info(ctx, "Starting gRPC order server",
				zap.String("address", listener.Addr().String()))
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
					logger.Error(ctx, "gRPC order server error", zap.Error(err))
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			server.GracefulStop()
			container.PostgresPool.Close()
			if err := container.RedisClient.Close(); err != nil {
				logger.Error(ctx, "failed to close redis", zap.Error(err))
			}

			return connection.Close()
		},
	})
}
