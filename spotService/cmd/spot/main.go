package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/app/spot"
	"go.uber.org/zap"
)

func main() {
	envPath := flag.String("env", ".env", "../../../.env")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		zapLogger.Fatal(context.Background(), "failed to load config",
			zap.Error(err))
	}

	app := spot.New(cfg.Spot)

	errChan, err := app.Start()
	if err != nil {
		zapLogger.Fatal(context.Background(), "failed to start spot service",
			zap.Error(err))
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		zapLogger.Info(context.Background(), "Shutting down spot server...")
	case err := <-errChan:
		zapLogger.Fatal(context.Background(), "Spot server failed",
			zap.Error(err))
	}

	if err := app.Stop(); err != nil {
		zapLogger.Error(context.Background(), "failed to stop spot server",
			zap.Error(err))
	}

	zapLogger.Info(context.Background(), "Spot server stopped")
}
