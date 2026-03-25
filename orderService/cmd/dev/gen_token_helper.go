package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/nastyazhadan/spot-order-grpc/shared/auth"
	authjwt "github.com/nastyazhadan/spot-order-grpc/shared/auth/jwt"
	"github.com/nastyazhadan/spot-order-grpc/shared/infrastructure/cache"
)

const (
	defaultUserID          = "550e8400-e29b-41d4-a716-446655440003"
	defaultAccessTokenTTL  = 5 * time.Minute
	defaultRefreshTokenTTL = 10 * time.Minute
	defaultRedisHost       = "localhost"
	defaultRedisPort       = "6379"
	defaultRedisTimeout    = 3 * time.Second
)

func main() {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}

	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret == "" {
		log.Fatal("JWT_SECRET environment variable must be set")
	}

	userID, err := uuid.Parse(defaultUserID)
	if err != nil {
		log.Fatalf("invalid default user id: %v", err)
	}

	jwtManager := authjwt.NewManager(
		secret,
		defaultAccessTokenTTL,
		defaultRefreshTokenTTL,
	)

	refreshJTI := uuid.NewString()

	accessToken, err := jwtManager.GenerateAccessToken(userID)
	if err != nil {
		log.Fatalf("failed to generate access token: %v", err)
	}

	refreshToken, err := jwtManager.GenerateRefreshToken(userID, refreshJTI)
	if err != nil {
		log.Fatalf("failed to generate refresh token: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultRedisTimeout)
	defer cancel()

	if err = saveRefreshToken(ctx, userID, refreshJTI, defaultRefreshTokenTTL); err != nil {
		log.Fatalf("failed to save refresh token in redis: %v", err)
	}

	fmt.Printf("access_token: %s\n", accessToken)
	fmt.Printf("refresh_token: %s\n", refreshToken)
}

func saveRefreshToken(ctx context.Context, userID uuid.UUID, jti string, ttl time.Duration) error {
	client := redis.NewClient(&redis.Options{
		Addr:         redisAddress(),
		DialTimeout:  defaultRedisTimeout,
		ReadTimeout:  defaultRedisTimeout,
		WriteTimeout: defaultRedisTimeout,
	})
	defer func() {
		_ = client.Close()
	}()

	store := cache.New(client)

	return auth.Save(ctx, store, ttl, userID, jti)
}

func redisAddress() string {
	host := firstNonEmpty(os.Getenv("REDIS_HOST"), defaultRedisHost)
	port := firstNonEmpty(os.Getenv("REDIS_PORT"), os.Getenv("EXTERNAL_REDIS_PORT"), defaultRedisPort)

	return fmt.Sprintf("%s:%s", host, port)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}

	return ""
}
