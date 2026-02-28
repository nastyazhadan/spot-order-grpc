package main

import (
	"context"
	"log"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	order.Run(context.Background(), cfg.Order)
}
