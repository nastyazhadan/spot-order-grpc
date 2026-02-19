package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/app/order"
	"github.com/nastyazhadan/spot-order-grpc/shared/config"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal(err)
	}

	app := order.New(cfg.Order)

	errChan, err := app.Start()
	if err != nil {
		log.Fatal(err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		log.Println("Shutting down order server...")
	case err := <-errChan:
		log.Printf("Order server failed: %v", err)
	}

	if err := app.Stop(); err != nil {
		log.Printf("failed to stop order server: %v", err)
	}

	log.Println("Order server stopped")
}
