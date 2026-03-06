package main

import (
	"context"
	"log"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	order.Run(context.Background(), cfg.Order)
}
