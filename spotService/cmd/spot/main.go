package main

import (
	"context"
	"log"

	"github.com/nastyazhadan/spot-order-grpc/spotService/config"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/application/spot"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	spot.Run(context.Background(), *cfg)
}
