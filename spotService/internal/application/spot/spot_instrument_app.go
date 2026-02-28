package spot

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/closer"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/xrequestid"
	wireGen "github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/spot/gen"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
)

func Run(ctx context.Context, cfg config.SpotConfig) {
	app := fx.New(
		fx.Supply(ctx, cfg),
		fx.Provide(
			provideContainer,
			provideListener,
			provideGRPCServer,
		),
		fx.Invoke(
			initLogger,
			registerCloser,
			startGRPCServer,
		),
	)

	app.Run()
}

func initLogger(cfg config.SpotConfig) error {
	return zapLogger.Init(cfg.LogLevel, cfg.LogFormat == "json")
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
		OnStop: func(context.Context) error {
			container.PostgresPool.Close()
			return container.RedisPool.Close()
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
		OnStop: func(context.Context) error {
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
	lifeCycle fx.Lifecycle,
	container *wireGen.Container,
) (*grpc.Server, error) {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return nil, fmt.Errorf("validate.ProtovalidateUnary: %w", err)
	}

	tracer := xrequestid.Server
	logger := logInterceptor.LoggerInterceptor()
	recoverer := recovery.PanicRecoveryInterceptor

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tracer,
			logger,
			recoverer,
			validator,
		),
	)

	reflection.Register(grpcServer)
	health.RegisterService(grpcServer)
	grpcSpot.Register(grpcServer, container.SpotService)

	lifeCycle.Append(fx.Hook{
		OnStop: func(context.Context) error {
			grpcServer.GracefulStop()
			return nil
		},
	})

	return grpcServer, nil
}

func registerCloser() {
	closer.SetLogger(zapLogger.Logger())

	closer.AddNamed("zap logger sync", func(ctx context.Context) error {
		zapLogger.Sync()
		return nil
	})
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
	})
}
