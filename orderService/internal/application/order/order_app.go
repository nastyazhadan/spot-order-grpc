package order

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	wireGen "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order/gen"
	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/auth"
	grpcErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	metricInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/rate_limit"
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
			provideTracingResource,
			provideClientConnection,
			provideGRPCClient,
			provideContainer,
			provideListener,
			provideGRPCServer,
		),
		fx.Invoke(
			registerLogger,
			registerTracing,
			registerMetrics,
			startGRPCServer,
		),
	)

	app.Run()
}

func registerLogger(lifeCycle fx.Lifecycle, cfg config.OrderConfig) error {
	zapLogger.Init(cfg.LogLevel, cfg.LogFormat == "json")

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return zapLogger.Sync()
		},
	})

	return nil
}

func provideTracingResource(ctx context.Context, cfg config.OrderConfig) (*resource.Resource, error) {
	return tracing.NewResource(ctx, cfg.Tracing)
}

func registerTracing(
	ctx context.Context,
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
	resource *resource.Resource,
) error {
	if err := tracing.InitTracer(ctx, cfg.Tracing, resource); err != nil {
		return err
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			if err := tracing.ShutdownTracer(ctx); err != nil {
				zapLogger.Error(ctx, "Failed to shutdown tracer", zap.Error(err))
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
) error {
	meterProvider, err := metricInterceptor.InitOpenTelemetry(ctx, cfg.Metrics, cfg.Tracing, resource)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:         cfg.Metrics.HTTPAddress,
		Handler:      promhttp.Handler(),
		ReadTimeout:  cfg.Metrics.ReadTimeout,
		WriteTimeout: cfg.Metrics.WriteTimeout,
		IdleTimeout:  cfg.Metrics.IdleTimeout,
	}

	lifeCycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if startErr := httpServer.ListenAndServe(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
					zapLogger.Error(ctx, "Failed to start metrics server", zap.Error(startErr))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			metrics.PushShutdownMetric(ctx, cfg.Metrics.PushGatewayURL, cfg.Tracing.ServiceName)
			if stopErr := meterProvider.Shutdown(ctx); stopErr != nil {
				zapLogger.Error(ctx, "Failed to shutdown metrics provider", zap.Error(stopErr))
			}
			return httpServer.Shutdown(ctx)
		},
	})

	return nil
}

func provideClientConnection(
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
) (*grpc.ClientConn, error) {
	connection, err := grpc.NewClient(
		cfg.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(tracing.UnaryClientInterceptor(cfg.Tracing.ServiceName)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                cfg.KeepAlive.PingTime,
			Timeout:             cfg.KeepAlive.PingTimeout,
			PermitWithoutStream: cfg.KeepAlive.PermitWithoutStream,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient: %w", err)
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return connection.Close()
		},
	})

	return connection, nil
}

func provideGRPCClient(
	ctx context.Context,
	connection *grpc.ClientConn,
	cfg config.OrderConfig,
) (*grpcClient.SpotClient, error) {
	client := grpcClient.NewSpotClient(connection, cfg.CircuitBreaker)

	checkCtx, cancel := context.WithTimeout(ctx, cfg.CheckTimeout)
	defer cancel()

	err := health.CheckHealth(checkCtx, connection)
	if err != nil {
		return nil, fmt.Errorf("spot connection at %s health check: %w", cfg.SpotAddress, err)
	}

	return client, nil
}

func provideContainer(
	ctx context.Context,
	lifeCycle fx.Lifecycle,
	marketClient *grpcClient.SpotClient,
	cfg config.OrderConfig,
) (*wireGen.Container, error) {
	container, err := wireGen.NewContainer(ctx, marketClient, cfg)
	if err != nil {
		return nil, err
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			container.PostgresPool.Close()
			return container.RedisClient.Close()
		},
	})

	return container, nil
}

func provideListener(
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
) (net.Listener, error) {
	listener, err := net.Listen("tcp", cfg.Address)
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
) (*grpc.Server, error) {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return nil, fmt.Errorf("validate.ProtovalidateUnary: %w", err)
	}

	tracer := tracing.UnaryServerInterceptor(cfg.Tracing.ServiceName)
	logger := logInterceptor.UnaryServerInterceptor()
	recoverer := recovery.UnaryServerInterceptor
	authenticator := auth.UnaryServerInterceptor(cfg.JWTSecret)
	errorsMapper := grpcErrors.UnaryServerInterceptor()
	rateLimiter := rate_limit.UnaryServerInterceptor(cfg.GRPCRateLimit, cfg.Tracing.ServiceName)
	meter := metricInterceptor.UnaryServerInterceptor(cfg.Tracing.ServiceName)

	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    cfg.KeepAlive.PingTime,
			Timeout: cfg.KeepAlive.PingTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             cfg.KeepAlive.MinPingInterval,
			PermitWithoutStream: cfg.KeepAlive.PermitWithoutStream,
		}),
		grpc.ChainUnaryInterceptor(
			rateLimiter, tracer, meter, logger, errorsMapper, recoverer, authenticator, validator,
		),
	)

	reflection.Register(grpcServer)
	health.RegisterService(grpcServer)
	grpcOrder.Register(grpcServer, container.OrderService)

	return grpcServer, nil
}

func startGRPCServer(
	lifeCycle fx.Lifecycle,
	server *grpc.Server,
	listener net.Listener,
) {
	lifeCycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			zapLogger.Info(ctx, fmt.Sprintf("Starting gRPC order server on %s", listener.Addr()))
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
					zapLogger.Error(ctx, "gRPC order server error", zap.Error(err))
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			server.GracefulStop()
			return nil
		},
	})
}
