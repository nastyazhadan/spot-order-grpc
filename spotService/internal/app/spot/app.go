package spot

import (
	"fmt"
	"log"
	"net"

	grpcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/grpc"
	svcSpot "github.com/nastyazhadan/spot-order-grpc/spotService/internal/services/spot"
	storage "github.com/nastyazhadan/spot-order-grpc/spotService/internal/storage/memory"

	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/validate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type App struct {
	grpcServer *grpc.Server
	listener   net.Listener
	addr       string
}

func New(addr string) (*App, error) {
	if addr == "" {
		addr = ":50052"
	}

	return &App{
		addr: addr,
	}, nil
}

func (a *App) Start() error {
	lis, err := net.Listen("tcp", a.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.addr, err)
	}
	a.listener = lis

	store := storage.NewMarketStore()
	useCase := svcSpot.NewService(store)

	a.grpcServer = grpc.NewServer()

	validator, err := validate.ProtovalidateUnary()
	if err != nil {
		return fmt.Errorf("proto validate: %w", err)
	}

	a.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(validator),
	)

	grpcSpot.Register(a.grpcServer, useCase)

	reflection.Register(a.grpcServer) // для отладки

	log.Printf("SpotInstrumentService listening on %s", a.addr)

	if err := a.grpcServer.Serve(a.listener); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

func (a *App) Stop() error {
	if a.grpcServer != nil {
		a.grpcServer.GracefulStop()
	}
	return nil
}
