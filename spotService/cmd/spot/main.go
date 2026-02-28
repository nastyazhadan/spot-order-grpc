package main

import (
	"context"
	"log"

	"github.com/joho/godotenv"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/spot"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	spot.Run(context.Background(), cfg.Spot)
}
