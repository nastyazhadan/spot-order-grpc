package main

import (
	"flag"

	"github.com/nastyazhadan/spot-order-grpc/spotService/internal/app/spot"
)

func main() {
	addr := flag.String("addr", "", "server address")
	flag.Parse()

	app, err := spot.New(*addr)
	if err != nil {
		panic(err)
	}

	if err := app.Start(); err != nil {
		panic(err)
	}
}
