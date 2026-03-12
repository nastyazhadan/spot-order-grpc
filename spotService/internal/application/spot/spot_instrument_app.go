package spot

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	grpcErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/rate_limit"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	wireGen "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/spot/gen"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
)

func Run(ctx context.Context, cfg config.SpotConfig) {
	app := fx.New(
		fx.Provide(
			func() context.Context {
				return ctx
			},
			func() config.SpotConfig {
				return cfg
			}),
		fx.Provide(
			provideContainer,
			provideListener,
			provideGRPCServer,
		),
		fx.Invoke(
			registerLogger,
			registerTracer,
			startGRPCServer,
		),
	)

	app.Run()
}

func registerLogger(lifeCycle fx.Lifecycle, cfg config.SpotConfig) error {
	zapLogger.Init(cfg.LogLevel, cfg.LogFormat == "json")

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return zapLogger.Sync()
		},
	})

	return nil
}

func registerTracer(ctx context.Context, lifeCycle fx.Lifecycle, cfg config.SpotConfig) error {
	err := tracing.InitTracer(ctx, cfg.Tracing)
	if err != nil {
		return err
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return tracing.ShutdownTracer(ctx)
		},
	})

	return nil
}

func provideContainer(
	ctx context.Context,
	lifeCycle fx.Lifecycle,
	cfg config.SpotConfig,
) (*wireGen.Container, error) {
	container, err := wireGen.NewContainer(ctx, cfg)
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
	cfg config.SpotConfig,
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
	cfg config.SpotConfig,
) (*grpc.Server, error) {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return nil, fmt.Errorf("spot: validate.ProtovalidateUnary: %w", err)
	}

	tracer := tracing.UnaryServerInterceptor(cfg.Tracing.ServiceName)
	logger := logInterceptor.UnaryServerInterceptor()
	recoverer := recovery.UnaryServerInterceptor
	errorsMapper := grpcErrors.UnaryServerInterceptor()
	rateLimiter := rate_limit.UnaryServerInterceptor(cfg.GRPCRateLimit)

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
			rateLimiter, tracer, logger, recoverer, errorsMapper, validator,
		),
	)

	reflection.Register(grpcServer)
	health.RegisterService(grpcServer)
	grpcSpot.Register(grpcServer, container.SpotService)

	return grpcServer, nil
}

func startGRPCServer(
	lifeCycle fx.Lifecycle,
	server *grpc.Server,
	listener net.Listener,
) {
	lifeCycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			zapLogger.Info(ctx, fmt.Sprintf("Starting gRPC spot server on %s", listener.Addr()))

			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
					zapLogger.Error(ctx, "gRPC spot server error", zap.Error(err))
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
