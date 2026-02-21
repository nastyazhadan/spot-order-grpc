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
	"github.com/nastyazhadan/spot-order-grpc/shared/infra/closer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"go.uber.org/zap"
)

const envFilePath = "../../../.env"

func main() {
	envPath := flag.String("env", envFilePath, "path to .env file")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		log.Fatalf("failed to load config file: %v", err)
	}

	appCtx, appCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer appCancel()
	defer gracefulShutdown(cfg.Order.GSTimeout)

	app, err := order.New(appCtx, cfg.Order)
	if err != nil {
		zapLogger.Error(appCtx, "failed to initialize order service", zap.Error(err))
		return
	}

	err = app.Start(appCtx)
	if err != nil {
		zapLogger.Error(appCtx, "failed to start order service", zap.Error(err))
		return
	}
}

func gracefulShutdown(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := closer.CloseAll(ctx); err != nil {
		zapLogger.Error(ctx, "failed to close all processes in order service", zap.Error(err))
	}
}
