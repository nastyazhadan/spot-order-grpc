package spot

import (
	"fmt"
	"log"
	"net"

	logInterceptor "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc/spot"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	storage "github.com/nastyazhadan/spot-order-grpc/spotService/internal/storage/memory"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer *grpc.Server
	listener   net.Listener
	address    string
}

func New(address string) (*App, error) {
	if address == "" {
		address = ":50052"
	}

	return &App{
		address: address,
	}, nil
}

func (app *App) Start() error {
	listener, err := net.Listen("tcp", app.address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", app.address, err)
	}
	app.listener = listener

	marketStore := storage.NewMarketStore()
	useCase := svcSpot.NewService(marketStore)

	app.grpcServer = grpc.NewServer()

	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("proto validate: %w", err)
	}

	logger := logInterceptor.LoggerInterceptor()

	app.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(validator),
	)

	app.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			validator,
			logger,
		),
	)

	grpcSpot.Register(app.grpcServer, useCase)

	reflection.Register(app.grpcServer) // для отладки

	log.Printf("SpotInstrumentService listening on %s", app.address)

	if err := app.grpcServer.Serve(app.listener); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

func (app *App) Stop() error {
	if app.grpcServer != nil {
		app.grpcServer.GracefulStop()
	}
	return nil
}
