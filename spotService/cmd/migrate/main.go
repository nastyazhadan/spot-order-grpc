package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/db"
	"github.com/nastyazhadan/spot-order-grpc/spotService/config"
	"github.com/nastyazhadan/spot-order-grpc/spotService/migrations"
)

func main() {
	cfg, err := config.LoadMigrate()
	if err != nil {
		log.Fatalf("failed to load migrate config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	migrationError := db.MigratePostgres(
		ctx,
		cfg.DBURI,
		migrations.Migrations,
		db.PoolConfig{
			MaxConnections:  cfg.PostgresPool.MaxConnections,
			MinConnections:  cfg.PostgresPool.MinConnections,
			MaxConnLifetime: cfg.PostgresPool.MaxConnLifetime,
			MaxConnIdleTime: cfg.PostgresPool.MaxConnIdleTime,
		},
	)
	if migrationError != nil {
		log.Fatalf("failed to migrate spot db: %v", migrationError)
	}

	log.Println("spot db migrations applied successfully")
}
