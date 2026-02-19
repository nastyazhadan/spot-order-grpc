package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/shared/config"
	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/app/spot"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal(err)
	}

	app := spot.New(cfg.Spot)

	errChan, err := app.Start()
	if err != nil {
		log.Fatal(err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		log.Println("Shutting down spot server...")
	case err := <-errChan:
		log.Printf("Spot server failed: %v", err)
	}

	if err := app.Stop(); err != nil {
		log.Printf("failed to stop spot server: %v", err)
	}

	log.Println("Spot server stopped")
}
