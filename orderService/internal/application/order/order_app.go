package order

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	wireGen "github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order/gen"
	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/health"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/xrequestid"
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
			provideSpotConnection,
			provideMarketClient,
			provideContainer,
			provideListener,
			provideGRPCServer,
		),
		fx.Invoke(
			registerLogger,
			startGRPCServer,
		),
	)

	app.Run()
}

func registerLogger(lifeCycle fx.Lifecycle, cfg config.OrderConfig) error {
	if err := zapLogger.Init(cfg.LogLevel, cfg.LogFormat == "json"); err != nil {
		return err
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			zapLogger.Sync()

			return nil
		},
	})

	return nil
}

func provideSpotConnection(
	lifeCycle fx.Lifecycle,
	cfg config.OrderConfig,
) (*grpc.ClientConn, error) {
	connection, err := grpc.NewClient(
		cfg.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(xrequestid.Client),
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

func provideMarketClient(
	connection *grpc.ClientConn,
	cfg config.OrderConfig,
) *grpcClient.Client {
	return grpcClient.New(connection, cfg.CircuitBreaker)
}

func provideContainer(
	ctx context.Context,
	lifeCycle fx.Lifecycle,
	marketClient *grpcClient.Client,
	cfg config.OrderConfig,
) (*wireGen.Container, error) {
	container, err := wireGen.NewContainer(ctx, marketClient, cfg)
	if err != nil {
		return nil, err
	}

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			container.PostgresPool.Close()
			return container.RedisPool.Close()
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
	grpcOrder.Register(grpcServer, container.OrderService)

	lifeCycle.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			grpcServer.GracefulStop()
			return nil
		},
	})

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
	})
}
