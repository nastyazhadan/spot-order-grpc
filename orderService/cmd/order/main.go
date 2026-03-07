package main

import (
	"context"
	"log"

	"github.com/nastyazhadan/spot-order-grpc/orderService/config"
	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/application/order"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	order.Run(context.Background(), *cfg)
}
