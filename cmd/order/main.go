package main

import (
	"log"
	"net"
	"os"
	"spotOrder/internal/storage/db"

	grpcClient "spotOrder/internal/clients/spot_instrument/grpc"
	grpcOrder "spotOrder/internal/grpc/order"
	svcOrder "spotOrder/internal/services/order"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := ":50051"

	spotAddr := os.Getenv("SPOT_INSTRUMENT_ADDR")
	if spotAddr == "" {
		spotAddr = "localhost:50052"
	}

	spotConn, err := grpc.NewClient(spotAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial spot instrument %s: %v", spotAddr, err)
	}
	defer spotConn.Close()

	marketClient := grpcClient.New(spotConn)

	store := db.NewOrderStore()
	useCase := svcOrder.NewService(store, marketClient)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen %s: %v", addr, err)
	}

	grpcServer := grpc.NewServer()
	grpcOrder.Register(grpcServer, useCase)

	log.Printf("OrderService listening on %s (spot instrument: %s)", addr, spotAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
