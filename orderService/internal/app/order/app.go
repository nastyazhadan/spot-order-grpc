package order

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	grpcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/grpc/order"
	svcOrder "github.com/nastyazhadan/spot-order-grpc/orderService/internal/services/order"
	storage "github.com/nastyazhadan/spot-order-grpc/orderService/internal/storage/memory"
	grpcClient "github.com/nastyazhadan/spot-order-grpc/shared/client/grpc"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer *grpc.Server
	listener   net.Listener

	address     string
	spotAddress string

	spotConnection *grpc.ClientConn
}

func New(address string) (*App, error) {
	if address == "" {
		address = "127.0.0.1:50051"
	}

	spotAddress := os.Getenv("SPOT_INSTRUMENT_ADDR")
	if spotAddress == "" {
		spotAddress = "127.0.0.1:50052"
	}

	return &App{
		address:     address,
		spotAddress: spotAddress,
	}, nil
}

func (app *App) Start() error {
	listener, err := net.Listen("tcp", app.address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", app.address, err)
	}
	app.listener = listener

	spotConnection, err := grpc.NewClient(
		app.spotAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial spot instrument %s: %w", app.spotAddress, err)
	}
	app.spotConnection = spotConnection

	marketClient := grpcClient.New(app.spotConnection)

	orderStore := storage.NewOrderStore()
	useCase := svcOrder.NewService(orderStore, orderStore, marketClient)

	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("proto validate: %w", err)
	}

	app.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(validator),
	)

	grpcOrder.Register(app.grpcServer, useCase)

	reflection.Register(app.grpcServer) // для отладки

	{
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if _, err := marketClient.ViewMarkets(ctx, []int32{}); err != nil {
			return fmt.Errorf("spot instrument not reachable at %s: %w", app.spotAddress, err)
		}
	}

	log.Printf("OrderService listening on %s (spot instrument: %s)", app.address, app.spotAddress)

	if err := app.grpcServer.Serve(app.listener); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

func (app *App) Stop() error {
	if app.grpcServer != nil {
		app.grpcServer.GracefulStop()
	}
	if app.spotConnection != nil {
		_ = app.spotConnection.Close()
	}
	if app.listener != nil {
		_ = app.listener.Close()
	}
	return nil
}
