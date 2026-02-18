package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/app/order"
)

func main() {
	address := flag.String("addr", "127.0.0.1:50051", "order server address")
	flag.Parse()

	app, err := order.New(*address)
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down order server...")

	if err := app.Stop(); err != nil {
		log.Printf("failed to stop order server: %v", err)
	}

	log.Println("Order server stopped")
}
