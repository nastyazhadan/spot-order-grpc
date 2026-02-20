package order

import (
	"context"
	"fmt"
	"net"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	storageOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/repository/postgres"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/postgres"
	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/recovery"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/xrequestid"
	"github.com/nastyazhadan/spot-order-grpc/shared/models"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer     *grpc.Server
	listener       net.Listener
	spotConnection *grpc.ClientConn
	dbPool         *pgxpool.Pool
	config         config.OrderConfig
}

func New(cfg config.OrderConfig) *App {
	return &App{
		config: cfg,
	}
}

func (app *App) Start(ctx context.Context) error {
	zapLogger.Init(app.config.LogLevel, app.config.LogFormat == "json")
	defer zapLogger.Sync()

	if err := app.setupDB(ctx); err != nil {
		return fmt.Errorf("setupDB: %w", err)
	}

	if err := app.setupListener(); err != nil {
		return fmt.Errorf("setupListener: %w", err)
	}

	if err := app.setupSpotConnection(); err != nil {
		return fmt.Errorf("setupSpotConnection: %w", err)
	}

	marketClient := grpcClient.New(app.spotConnection, app.config.CircuitBreaker)

	if err := app.checkSpotReachable(ctx, marketClient); err != nil {
		return fmt.Errorf("checkSpotReachable: %w", err)
	}

	if err := app.setupGRPCServer(ctx, marketClient); err != nil {
		return fmt.Errorf("setupGRPCServer: %w", err)
	}

	errChan := make(chan error, 1)

	go func() {
		zapLogger.Info(ctx, "OrderService started",
			zap.String("address", app.config.Address),
		)

		if err := app.grpcServer.Serve(app.listener); err != nil {
			errChan <- err
		}
	}()

	return nil
}

func (app *App) setupDB(ctx context.Context) error {
	pool, err := postgres.SetupDB(ctx, app.config.DBURI, app.config.MigrationDir)
	if err != nil {
		return err
	}

	app.dbPool = pool
	return nil
}

func (app *App) setupListener() error {
	listener, err := net.Listen("tcp", app.config.Address)
	if err != nil {
		return fmt.Errorf("net.Listen: %w", err)
	}

	app.listener = listener
	return nil
}

func (app *App) setupSpotConnection() error {
	connection, err := grpc.NewClient(
		app.config.SpotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(xrequestid.Client),
	)

	if err != nil {
		return fmt.Errorf("grpc.NewClient: %w", err)
	}

	app.spotConnection = connection
	return nil
}

func (app *App) checkSpotReachable(ctx context.Context, client *grpcClient.Client) error {
	ctx, cancel := context.WithTimeout(ctx, app.config.CheckTimeout)
	defer cancel()

	if _, err := client.ViewMarkets(ctx, []models.UserRole{models.UserRoleUser}); err != nil {
		return fmt.Errorf("spot instrument is not reachable at %s: %w", app.config.SpotAddress, err)
	}

	return nil
}

func (app *App) setupGRPCServer(ctx context.Context, client *grpcClient.Client) error {
	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("validate.ProtovalidateUnary: %w", err)
	}

	tracer := xrequestid.Server
	logger := logInterceptor.LoggerInterceptor()
	recoverer := recovery.PanicRecoveryInterceptor

	app.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tracer,
			logger,
			recoverer,
			validator,
		),
	)

	orderStore := storageOrder.NewOrderStore(app.dbPool)
	useCase := svcOrder.NewService(orderStore, orderStore, client, app.config.CreateTimeout)
	grpcOrder.Register(app.grpcServer, useCase)
	reflection.Register(app.grpcServer)

	return nil
}
