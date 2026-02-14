package order

import (
	"context"
	"fmt"
	grpcClient "github.com/nastyazhadan/spotOrder/internal/client/grpc"
	"log"
	"net"
	"os"
	grpcOrder "spotOrder/internal/grpc/order"
	svcOrder "spotOrder/internal/services/order"
	"spotOrder/internal/storage/memory"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer *grpc.Server
	listener   net.Listener

	addr     string
	spotAddr string

	spotConn *grpc.ClientConn
}

func New(addr string) (*App, error) {
	if addr == "" {
		addr = "127.0.0.1:50051"
	}

	spotAddr := os.Getenv("SPOT_INSTRUMENT_ADDR")
	if spotAddr == "" {
		spotAddr = "127.0.0.1:50052"
	}

	return &App{
		addr:     addr,
		spotAddr: spotAddr,
	}, nil
}

func (a *App) Start() error {
	lis, err := net.Listen("tcp", a.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.addr, err)
	}
	a.listener = lis

	spotConn, err := grpc.NewClient(
		a.spotAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial spot instrument %s: %w", a.spotAddr, err)
	}
	a.spotConn = spotConn

	marketClient := grpcClient.New(a.spotConn)

	store := memory.NewOrderStore()
	useCase := svcOrder.NewService(store, store, marketClient)

	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("proto validate: %w", err)
	}

	a.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(validator),
	)

	grpcOrder.Register(a.grpcServer, useCase)

	reflection.Register(a.grpcServer) // для отладки

	{
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if _, err := marketClient.ViewMarkets(ctx, []int32{}); err != nil {
			return fmt.Errorf("spot instrument not reachable at %s: %w", a.spotAddr, err)
		}
	}

	log.Printf("OrderService listening on %s (spot instrument: %s)", a.addr, a.spotAddr)

	if err := a.grpcServer.Serve(a.listener); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

func (a *App) Stop() error {
	if a.grpcServer != nil {
		a.grpcServer.GracefulStop()
	}
	if a.spotConn != nil {
		_ = a.spotConn.Close()
	}
	if a.listener != nil {
		_ = a.listener.Close()
	}
	return nil
}
