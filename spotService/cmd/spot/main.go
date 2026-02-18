package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/app/spot"
)

func main() {
	address := flag.String("addr", "", "server address")
	flag.Parse()

	app, err := spot.New(*address)
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down spot server...")

	if err := app.Stop(); err != nil {
		log.Printf("failed to stop spot server: %v", err)
	}

	log.Println("Spot server stopped")
}
