package order

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/retry"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

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
)

var GRPCProviders = fx.Options(
	fx.Provide(
		health.NewServer(),
		provideClientConnection,
		provideGRPCClient,

		provideMarketViewer,
		provideListener,
		provideGRPCServer,
	),
	fx.Invoke(
		startGRPCServer,
	),
)

func provideClientConnection(
	cfg config.OrderConfig,
) (*grpc.ClientConn, error) {
	connection, err := grpc.NewClient(
		cfg.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			auth.UnaryClientAuthInterceptor(),
			tracing.UnaryClientInterceptor(),
			retry.UnaryClientInterceptor(
				retry.WithMax(cfg.Retry.MaxAttempts),
				retry.WithBackoff(retry.BackoffExponentialWithJitter(cfg.Retry.InitialBackoff, cfg.Retry.Jitter)),
				retry.WithCodes(codes.DeadlineExceeded, codes.Unavailable),
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
	connection *grpc.ClientConn,
	cfg config.OrderConfig,
	logger *zapLogger.Logger,
) *grpcClient.SpotClient {
	return grpcClient.NewSpotClient(connection, cfg.CircuitBreaker, logger)
}

func provideMarketViewer(client *grpcClient.SpotClient) orderService.MarketViewer {
	return client
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
	container *container,
	cfg config.OrderConfig,
	appLogger *zapLogger.Logger,
	healthServer *health.Server,
) (*grpc.Server, error) {
	validator, err := validate.UnaryServerInterceptor()
	if err != nil {
		return nil, err
	}
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
	health.RegisterService(grpcServer, healthServer)
	grpcAuth.Register(grpcServer, container.AuthService)
	grpcOrder.Register(grpcServer, container.OrderService, appLogger)

	return grpcServer, nil
}

func startGRPCServer(
	in appCtxIn,
	lifeCycle fx.Lifecycle,
	server *grpc.Server,
	listener net.Listener,
	logger *zapLogger.Logger,
	healthServer *health.Server,
) {
	appCtx := in.AppCtx

	lifeCycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			logger.Info(startCtx, "Starting gRPC order server",
				zap.String("address", listener.Addr().String()))

			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
					logger.Error(appCtx, "gRPC order server stopped unexpectedly", zap.Error(err))
				}
			}()

			healthServer.SetServing()

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			healthServer.SetNotServing()

			return stopGRPCServer(stopCtx, server, logger, "order")
		},
	})
}

func stopGRPCServer(
	ctx context.Context,
	server *grpc.Server,
	logger *zapLogger.Logger,
	serviceName string,
) error {
	done := make(chan struct{})

	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		logger.Info(ctx, "gRPC server stopped gracefully",
			zap.String("service", serviceName))
		return nil

	case <-ctx.Done():
		logger.Warn(ctx, "gRPC graceful shutdown timed out, forcing stop",
			zap.String("service", serviceName),
			zap.Error(ctx.Err()),
		)
		server.Stop()

		<-done

		return fmt.Errorf("gRPC graceful shutdown timed out: service=%s: %v", serviceName, ctx.Err())
	}
}
