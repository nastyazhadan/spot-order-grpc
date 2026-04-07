package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/orderService/config"
	"github.com/nastyazhadan/spot-order-grpc/orderService/migrations"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	migrationError := db.MigratePostgres(
		ctx,
		cfg.Service.DBURI,
		migrations.Migrations,
		db.PoolConfig{
			MaxConnections:  cfg.PostgresPool.MaxConnections,
			MinConnections:  cfg.PostgresPool.MinConnections,
			MaxConnLifetime: cfg.PostgresPool.MaxConnLifetime,
			MaxConnIdleTime: cfg.PostgresPool.MaxConnIdleTime,
		},
	)
	if migrationError != nil {
		log.Fatalf("failed to migrate order db: %v", err)
	}

	log.Println("order db migrations applied successfully")
}
