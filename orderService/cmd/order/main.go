package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/closer"
	zapLogger "github.com/nastyazhadan/spot-order-grpc/shared/interceptors/logger/zap"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
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
