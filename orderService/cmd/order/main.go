package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/app/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/interceptors/closer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"go.uber.org/zap"
)

const envFilePath = "../../../.env"

func main() {
	envPath := flag.String("env", ".env", envFilePath)
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		log.Fatalf("failed to load config file: %v", err)
	}

	appCtx, appCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer appCancel()
	defer gracefulShutdown(cfg.Order.GSTimeout)

	closer.Configure(syscall.SIGINT, syscall.SIGTERM)

	app := order.New(cfg.Order)

	err = app.Start(appCtx)
	if err != nil {
		zapLogger.Fatal(appCtx, "failed to start app",
			zap.Error(err))
	}
}

func gracefulShutdown(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := closer.CloseAll(ctx); err != nil {
		zapLogger.Error(ctx, "failed to close all processes in order server",
			zap.Error(err))
	}

	zapLogger.Info(ctx, "Order server stopped")
}
