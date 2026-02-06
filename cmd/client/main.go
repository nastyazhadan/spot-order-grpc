package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	spot_orderv1 "github.com/nastyazhadan/protos/gen/go/spot_order"
	"google.golang.org/genproto/googleapis/type/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	op := flag.String("op", "create", "operation: create or get")
	addr := flag.String("addr", "localhost:50051", "order service address")

	userID := flag.String("user", "", "user id (uuid)")
	marketID := flag.String("market", "", "market id")
	quantity := flag.Int64("quantity", 0, "quantity")
	price := flag.String("price", "", "price decimal string, e.g. 123.45")

	orderID := flag.String("order", "", "order id (uuid) for get")
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial %s: %v", *addr, err)
	}
	defer conn.Close()

	client := spot_orderv1.NewOrderServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch *op {
	case "create":
		resp, err := client.CreateOrder(ctx, &spot_orderv1.CreateOrderRequest{
			UserId:    *userID,
			MarketId:  *marketID,
			OrderType: spot_orderv1.OrderType_TYPE_LIMIT,
			Price:     &decimal.Decimal{Value: *price},
			Quantity:  *quantity,
		})
		if err != nil {
			log.Fatalf("CreateOrder: %v", err)
		}
		fmt.Printf("order_id = %s, status = %s\n", resp.GetOrderId(), resp.GetStatus().String())

	case "get":
		resp, err := client.GetOrderStatus(ctx, &spot_orderv1.GetOrderStatusRequest{
			OrderId: *orderID,
			UserId:  *userID,
		})
		if err != nil {
			log.Fatalf("GetOrderStatus: %v", err)
		}

		fmt.Printf("status = %s\n", resp.GetStatus().String())

	default:
		log.Fatalf("unknown operation: %s (use create or get)", *op)
	}
}
