package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/spotService/config"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/application"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	appCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application.Run(appCtx, *cfg)
}
