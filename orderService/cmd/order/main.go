package main

import (
	"flag"
	"log"

	"github.com/nastyazhadan/spot-order-grpc/orderService/internal/app/order"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:50051", "order server address")
	flag.Parse()

	app, err := order.New(*addr)
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
