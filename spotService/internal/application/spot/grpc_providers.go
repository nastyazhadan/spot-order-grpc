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
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/auth"
	grpcErrors "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/errors"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logging/zap"
	metricInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/metrics"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/ratelimit"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/tracing"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
)

var GRPCProviders = fx.Options(
	fx.Provide(
		health.NewServer,
		provideListener,
		provideGRPCServer,
	),
	fx.Invoke(
		startGRPCServer,
	),
)

func provideListener(
	lifeCycle fx.Lifecycle,
	cfg config.SpotConfig,
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
	cfg config.SpotConfig,
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
	authenticator := auth.UnaryServerInterceptor(container.JWTManager, container.SessionStore, cfg.AuthVerifier)
	errorsMapper := grpcErrors.UnaryServerInterceptor(appLogger)
	rateLimiter := ratelimit.SpotUnaryServerInterceptor(cfg, appLogger)
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
		grpc.ChainUnaryInterceptor(
			recoverer, tracer, meter, logger, authenticator, rateLimiter, errorsMapper, validator,
		),
	)

	reflection.Register(grpcServer)
	health.RegisterService(grpcServer, healthServer)
	grpcSpot.Register(grpcServer, container.SpotService)

	return grpcServer, nil
}

func startGRPCServer(
	in appCtxIn,
	lifeCycle fx.Lifecycle,
	server *grpc.Server,
	listener net.Listener,
	logger *zapLogger.Logger,
) {
	appCtx := in.AppCtx

	lifeCycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			logger.Info(startCtx, "Starting gRPC spot server",
				zap.String("address", listener.Addr().String()))

			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
					logger.Error(appCtx, "gRPC spot server stopped unexpectedly", zap.Error(err))
				}
			}()

			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			return stopGRPCServer(stopCtx, server, logger, "spot")
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
