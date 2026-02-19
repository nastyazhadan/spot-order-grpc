package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/app/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"go.uber.org/zap"
)

func main() {
	envPath := flag.String("env", ".env", "../../../.env")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		zapLogger.Fatal(context.Background(), "failed to load config file",
			zap.Error(err))
	}

	app := order.New(cfg.Order)

	errChan, err := app.Start()
	if err != nil {
		zapLogger.Fatal(context.Background(), "failed to start app",
			zap.Error(err))
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		zapLogger.Info(context.Background(), "Received signal, shutting down order server...")
	case err := <-errChan:
		zapLogger.Fatal(context.Background(), "Order server failed",
			zap.Error(err),
		)
	}

	if err := app.Stop(); err != nil {
		zapLogger.Error(context.Background(), "failed to stop order server",
			zap.Error(err),
		)
	}
	zapLogger.Info(context.Background(), "Order server stopped")
}
