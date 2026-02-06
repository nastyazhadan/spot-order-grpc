package main

import (
	"log"
	"net"
	grpcSpotInstrument "spotOrder/internal/grpc/spot_instrument"
	"spotOrder/internal/services/spot_instrument"
	"spotOrder/internal/storage/db"

	"google.golang.org/grpc"
)

func main() {
	addr := ":50052"

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	store := db.NewMarketStore()
	useCase := spot_instrument.NewService(store)

	grpcServer := grpc.NewServer()
	grpcSpotInstrument.Register(grpcServer, useCase)

	log.Printf("SpotInstrumentService listening on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
